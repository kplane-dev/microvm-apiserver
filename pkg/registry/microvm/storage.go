// Package microvm implements the REST storage for MicroVM resources.
// Data lives in memory, backed by runtime provider calls. No etcd.
package microvm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kplane-dev/microvm-apiserver/pkg/apis/microvms/v1alpha1"
	"github.com/kplane-dev/microvm-apiserver/pkg/provider"

	"k8s.io/apimachinery/pkg/api/errors"
	metainternalversion "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/registry/rest"
)

var _ rest.Storage = &Storage{}
var _ rest.Creater = &Storage{}
var _ rest.Getter = &Storage{}
var _ rest.Lister = &Storage{}
var _ rest.GracefulDeleter = &Storage{}
var _ rest.Scoper = &Storage{}

// Storage implements the REST storage for MicroVM resources.
// All state is in-memory, backed by provider calls to agents.
type Storage struct {
	mu       sync.RWMutex
	vms      map[string]*v1alpha1.MicroVM // key = "namespace/name"
	provider provider.RuntimeProvider
}

// NewStorage creates a new MicroVM storage with the given provider.
func NewStorage(p provider.RuntimeProvider) *Storage {
	return &Storage{
		vms:      make(map[string]*v1alpha1.MicroVM),
		provider: p,
	}
}

func (s *Storage) New() runtime.Object {
	return &v1alpha1.MicroVM{}
}

func (s *Storage) Destroy() {}

func (s *Storage) NewList() runtime.Object {
	return &v1alpha1.MicroVMList{}
}

func (s *Storage) NamespaceScoped() bool {
	return true
}

func key(namespace, name string) string {
	return namespace + "/" + name
}

func (s *Storage) Get(ctx context.Context, name string, options *metav1.GetOptions) (runtime.Object, error) {
	namespace := genericNamespace(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()

	vm, ok := s.vms[key(namespace, name)]
	if !ok {
		return nil, errors.NewNotFound(v1alpha1.Resource("microvms"), name)
	}
	return vm.DeepCopy(), nil
}

func (s *Storage) List(ctx context.Context, options *metainternalversion.ListOptions) (runtime.Object, error) {
	namespace := genericNamespace(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := &v1alpha1.MicroVMList{}
	for _, vm := range s.vms {
		if namespace != "" && vm.Namespace != namespace {
			continue
		}
		list.Items = append(list.Items, *vm.DeepCopy())
	}
	return list, nil
}

func (s *Storage) Create(ctx context.Context, obj runtime.Object, createValidation rest.ValidateObjectFunc, options *metav1.CreateOptions) (runtime.Object, error) {
	vm := obj.(*v1alpha1.MicroVM)
	namespace := genericNamespace(ctx)
	vm.Namespace = namespace

	if vm.Name == "" {
		return nil, errors.NewBadRequest("name is required")
	}
	if vm.Spec.Image == "" {
		return nil, errors.NewBadRequest("spec.image is required")
	}
	if vm.Spec.Runtime == "" {
		vm.Spec.Runtime = "firecracker"
	}

	k := key(namespace, vm.Name)
	s.mu.Lock()
	if _, exists := s.vms[k]; exists {
		s.mu.Unlock()
		return nil, errors.NewAlreadyExists(v1alpha1.Resource("microvms"), vm.Name)
	}
	s.mu.Unlock()

	// Create the VM via the provider
	result, err := s.provider.Claim(ctx, provider.ClaimRequest{
		Image:     vm.Spec.Image,
		CPUMillis: vm.Spec.Resources.CPU,
		MemoryMiB: vm.Spec.Resources.MemoryMiB,
	})
	if err != nil {
		return nil, errors.NewInternalError(fmt.Errorf("provider claim: %w", err))
	}

	vm.Status = v1alpha1.MicroVMStatus{
		Phase:    v1alpha1.MicroVMRunning,
		NodeName: result.NodeIP,
		VMID:     result.VMID,
		IP:       result.GuestIP,
	}
	vm.CreationTimestamp = metav1.NewTime(time.Now())

	s.mu.Lock()
	s.vms[k] = vm.DeepCopy()
	s.mu.Unlock()

	return vm, nil
}

func (s *Storage) Delete(ctx context.Context, name string, deleteValidation rest.ValidateObjectFunc, options *metav1.DeleteOptions) (runtime.Object, bool, error) {
	namespace := genericNamespace(ctx)
	k := key(namespace, name)

	s.mu.Lock()
	vm, ok := s.vms[k]
	if !ok {
		s.mu.Unlock()
		return nil, false, errors.NewNotFound(v1alpha1.Resource("microvms"), name)
	}
	delete(s.vms, k)
	s.mu.Unlock()

	// Release via provider
	if err := s.provider.Release(ctx, vm.Status.VMID); err != nil {
		// Log but don't fail — the VM is already removed from our map
		fmt.Printf("warn: provider release %s: %v\n", vm.Status.VMID, err)
	}

	vm.Status.Phase = v1alpha1.MicroVMTerminated
	return vm, true, nil
}

func (s *Storage) ConvertToTable(ctx context.Context, obj runtime.Object, tableOptions runtime.Object) (*metav1.Table, error) {
	return rest.NewDefaultTableConvertor(schema.GroupResource{
		Group:    v1alpha1.GroupName,
		Resource: "microvms",
	}).ConvertToTable(ctx, obj, tableOptions)
}

func (s *Storage) GetSingularName() string {
	return "microvm"
}

// GetVM returns a VM by namespace/name from the in-memory store (for subresources).
func (s *Storage) GetVM(namespace, name string) (*v1alpha1.MicroVM, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vm, ok := s.vms[key(namespace, name)]
	if !ok {
		return nil, false
	}
	return vm.DeepCopy(), true
}

// TrackVM adds a VM to the in-memory store (for branches).
func (s *Storage) TrackVM(vm *v1alpha1.MicroVM) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vms[key(vm.Namespace, vm.Name)] = vm.DeepCopy()
}

func genericNamespace(ctx context.Context) string {
	// Extract namespace from the request context
	// k8s.io/apiserver sets this via the request info
	if ri, ok := requestInfoFrom(ctx); ok {
		return ri.Namespace
	}
	return ""
}

// requestInfoFrom extracts the request info from context.
// This is a simplified version — the real one uses k8s.io/apiserver/pkg/endpoints/request.
func requestInfoFrom(ctx context.Context) (*requestInfo, bool) {
	val := ctx.Value(requestInfoKey)
	if val == nil {
		return nil, false
	}
	ri, ok := val.(*requestInfo)
	return ri, ok
}

type requestInfo struct {
	Namespace string
}

type contextKey string

const requestInfoKey contextKey = "requestInfo"
