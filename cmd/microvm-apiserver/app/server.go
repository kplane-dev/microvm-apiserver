package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/kplane-dev/microvm-apiserver/pkg/apis/microvms/v1alpha1"
	"github.com/kplane-dev/microvm-apiserver/pkg/provider/firecracker"
	"github.com/kplane-dev/microvm-apiserver/pkg/registry/microvm"

	"k8s.io/klog/v2"
)

// Server is the MicroVM API server.
type Server struct {
	listenAddr string
	storage    *microvm.Storage
	provider   *firecracker.Provider
}

// NewServer creates a new MicroVM API server.
func NewServer(listenAddr string, agents []string) *Server {
	p := firecracker.New()
	p.SetAgents(agents)

	return &Server{
		listenAddr: listenAddr,
		storage:    microvm.NewStorage(p),
		provider:   p,
	}
}

// Run starts the server and blocks until the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()

	// MicroVM CRUD — follows Kubernetes API conventions on the path structure
	// /apis/microvms.kplane.dev/v1alpha1/namespaces/{ns}/microvms
	mux.HandleFunc("/apis/microvms.kplane.dev/v1alpha1/namespaces/", s.handleMicroVMs)
	mux.HandleFunc("/apis/microvms.kplane.dev/v1alpha1/microvms", s.handleListAll)

	// API discovery
	mux.HandleFunc("/apis/microvms.kplane.dev/v1alpha1", s.handleAPIResources)
	mux.HandleFunc("/apis/microvms.kplane.dev", s.handleAPIGroup)

	// Health
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
	})

	// Provider status
	mux.HandleFunc("/provider/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := s.provider.AgentStatus(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(status)
	})

	// Agent discovery
	go s.discoverAgents(ctx)

	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	klog.Infof("microvm-apiserver listening on %s", s.listenAddr)

	server := &http.Server{Handler: mux}
	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.Serve(ln)
}

func (s *Server) handleAPIGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"kind":       "APIGroup",
		"apiVersion": "v1",
		"name":       v1alpha1.GroupName,
		"versions": []map[string]string{
			{"groupVersion": v1alpha1.GroupName + "/v1alpha1", "version": "v1alpha1"},
		},
		"preferredVersion": map[string]string{
			"groupVersion": v1alpha1.GroupName + "/v1alpha1", "version": "v1alpha1",
		},
	})
}

func (s *Server) handleAPIResources(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"kind":         "APIResourceList",
		"apiVersion":   "v1",
		"groupVersion": v1alpha1.GroupName + "/v1alpha1",
		"resources": []map[string]interface{}{
			{
				"name":       "microvms",
				"singularName": "microvm",
				"namespaced": true,
				"kind":       "MicroVM",
				"verbs":      []string{"get", "list", "create", "delete"},
			},
		},
	})
}

func (s *Server) handleListAll(w http.ResponseWriter, r *http.Request) {
	vms, err := s.storage.ListAll(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vms)
}

func (s *Server) handleMicroVMs(w http.ResponseWriter, r *http.Request) {
	// Parse: /apis/microvms.kplane.dev/v1alpha1/namespaces/{ns}/microvms[/{name}[/{subresource}]]
	path := r.URL.Path
	prefix := "/apis/microvms.kplane.dev/v1alpha1/namespaces/"
	remainder := path[len(prefix):]

	// Split: "ns/microvms/name/subresource"
	parts := splitPath(remainder)
	if len(parts) < 2 || parts[1] != "microvms" {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	namespace := parts[0]
	name := ""
	subresource := ""
	if len(parts) >= 3 {
		name = parts[2]
	}
	if len(parts) >= 4 {
		subresource = parts[3]
	}

	switch {
	case r.Method == "POST" && name == "":
		s.handleCreate(w, r, namespace)
	case r.Method == "GET" && name == "":
		s.handleList(w, r, namespace)
	case r.Method == "GET" && name != "" && subresource == "":
		s.handleGet(w, r, namespace, name)
	case r.Method == "DELETE" && name != "":
		s.handleDelete(w, r, namespace, name)
	case r.Method == "POST" && subresource == "exec":
		s.handleExec(w, r, namespace, name)
	case r.Method == "POST" && subresource == "branch":
		s.handleBranch(w, r, namespace, name)
	case r.Method == "POST" && subresource == "ssh":
		s.handleSSH(w, r, namespace, name)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request, namespace string) {
	var vm v1alpha1.MicroVM
	if err := json.NewDecoder(r.Body).Decode(&vm); err != nil {
		http.Error(w, fmt.Sprintf("decode: %v", err), http.StatusBadRequest)
		return
	}
	vm.Namespace = namespace

	result, err := s.storage.CreateVM(r.Context(), &vm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleGet(w http.ResponseWriter, r *http.Request, namespace, name string) {
	vm, ok := s.storage.GetVM(namespace, name)
	if !ok {
		http.Error(w, fmt.Sprintf("microvm %q not found", name), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(vm)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, namespace string) {
	list := s.storage.ListVMs(namespace)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, namespace, name string) {
	if err := s.storage.DeleteVM(r.Context(), namespace, name); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}

func (s *Server) handleExec(w http.ResponseWriter, r *http.Request, namespace, name string) {
	vm, ok := s.storage.GetVM(namespace, name)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var opts v1alpha1.MicroVMExecOptions
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, fmt.Sprintf("decode: %v", err), http.StatusBadRequest)
		return
	}
	result, err := s.provider.Exec(r.Context(), vm.Status.VMID, opts.Command)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) handleBranch(w http.ResponseWriter, r *http.Request, namespace, name string) {
	vm, ok := s.storage.GetVM(namespace, name)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var opts v1alpha1.MicroVMBranchOptions
	if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
		http.Error(w, fmt.Sprintf("decode: %v", err), http.StatusBadRequest)
		return
	}

	branches, err := s.provider.Branch(r.Context(), vm.Status.VMID, opts.Count)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Register branches as new MicroVM resources
	var branchVMs []*v1alpha1.MicroVM
	for i, b := range branches {
		branchVM := &v1alpha1.MicroVM{}
		branchVM.Name = fmt.Sprintf("%s-branch-%d", name, i)
		branchVM.Namespace = namespace
		branchVM.Spec = vm.Spec
		branchVM.Status = v1alpha1.MicroVMStatus{
			Phase:    v1alpha1.MicroVMRunning,
			NodeName: vm.Status.NodeName,
			VMID:     b.VMID,
			IP:       b.GuestIP,
		}
		s.storage.TrackVM(branchVM)
		branchVMs = append(branchVMs, branchVM)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"branches": branchVMs,
	})
}

func (s *Server) handleSSH(w http.ResponseWriter, r *http.Request, namespace, name string) {
	vm, ok := s.storage.GetVM(namespace, name)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	action := r.URL.Query().Get("action")
	switch action {
	case "enable":
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "key required", http.StatusBadRequest)
			return
		}
		ip, port, err := s.provider.EnableSSH(r.Context(), vm.Status.VMID, key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ip": ip, "port": port, "status": "enabled",
		})
	case "disable":
		if err := s.provider.DisableSSH(r.Context(), vm.Status.VMID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "disabled"})
	default:
		http.Error(w, "action must be enable or disable", http.StatusBadRequest)
	}
}

func (s *Server) discoverAgents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
			// TODO: use Kubernetes endpoint discovery
		}
	}
}

func splitPath(path string) []string {
	var parts []string
	for _, p := range split(path, '/') {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return parts
}

func split(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
