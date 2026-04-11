package app

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/kplane-dev/microvm-apiserver/pkg/apis/microvms/v1alpha1"
	"github.com/kplane-dev/microvm-apiserver/pkg/provider/firecracker"
	"github.com/kplane-dev/microvm-apiserver/pkg/registry/microvm"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apiserver/pkg/registry/rest"
	genericapiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/klog/v2"
)

var (
	scheme = runtime.NewScheme()
	codecs = serializer.NewCodecFactory(scheme)
)

func init() {
	v1alpha1.AddToScheme(scheme)
}

// Config holds the configuration for the MicroVM API server.
type Config struct {
	GenericConfig *genericapiserver.RecommendedConfig
	Agents        []string // firecracker-agent URLs
}

// MicroVMServer is the API server for MicroVM resources.
type MicroVMServer struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

// Complete fills in defaults and returns a completed config.
func (c *Config) Complete() CompletedConfig {
	c.GenericConfig.Complete()
	return CompletedConfig{c}
}

type CompletedConfig struct {
	*Config
}

// New creates a new MicroVM API server.
func (c CompletedConfig) New() (*MicroVMServer, error) {
	genericServer, err := c.GenericConfig.Complete().New("microvm-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, fmt.Errorf("create generic server: %w", err)
	}

	// Create the Firecracker provider
	fcProvider := firecracker.New()
	fcProvider.SetAgents(c.Agents)

	// Start agent discovery in background
	go discoverAgents(fcProvider, c.Agents)

	// Create storage
	vmStorage := microvm.NewStorage(fcProvider)

	// Install the API group
	apiGroupInfo := genericapiserver.NewDefaultAPIGroupInfo(
		v1alpha1.GroupName,
		scheme,
		runtime.NewParameterCodec(scheme),
		codecs,
	)
	apiGroupInfo.VersionedResourcesStorageMap[v1alpha1.SchemeGroupVersion.Version] = map[string]rest.Storage{
		"microvms": vmStorage,
	}

	if err := genericServer.InstallAPIGroup(&apiGroupInfo); err != nil {
		return nil, fmt.Errorf("install api group: %w", err)
	}

	// Add health endpoint for the provider
	genericServer.Handler.NonGoRestfulMux.HandleFunc("/provider/status", func(w http.ResponseWriter, r *http.Request) {
		status, err := fcProvider.AgentStatus(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		fmt.Fprintf(w, "agents=%d active=%d warm=%v", status.Agents, status.Active, status.Warm)
	})

	return &MicroVMServer{GenericAPIServer: genericServer}, nil
}

// discoverAgents periodically updates the agent list.
// TODO: use Kubernetes endpoint discovery instead of static list.
func discoverAgents(p *firecracker.Provider, initial []string) {
	p.SetAgents(initial)
	for {
		time.Sleep(30 * time.Second)
		// For now, just re-validate existing agents
		var healthy []string
		for _, agent := range initial {
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Get(agent + "/healthz")
			if err == nil && resp.StatusCode == 200 {
				healthy = append(healthy, agent)
			}
		}
		if len(healthy) != len(initial) {
			klog.Infof("agent discovery: %d/%d healthy", len(healthy), len(initial))
		}
		p.SetAgents(healthy)
	}
}

// Run starts the API server.
func Run(ctx context.Context, agents []string, listenAddr string) error {
	config := &genericapiserver.RecommendedConfig{
		Config: *genericapiserver.NewConfig(codecs),
	}

	// Minimal config for development/testing
	config.SecureServing = nil // disable TLS for now
	config.ExternalAddress = listenAddr

	cfg := &Config{
		GenericConfig: config,
		Agents:        agents,
	}

	server, err := cfg.Complete().New()
	if err != nil {
		return err
	}

	return server.GenericAPIServer.PrepareRun().Run(ctx.Done())
}
