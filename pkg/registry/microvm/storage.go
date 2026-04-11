// Package microvm implements the in-memory storage for MicroVM resources.
// Data backed by runtime provider calls. No etcd.
package microvm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kplane-dev/microvm-apiserver/pkg/apis/microvms/v1alpha1"
	"github.com/kplane-dev/microvm-apiserver/pkg/provider"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Storage manages MicroVM resources in memory, backed by provider calls.
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

func key(namespace, name string) string {
	return namespace + "/" + name
}

// CreateVM provisions a VM via the provider and stores it.
func (s *Storage) CreateVM(ctx context.Context, vm *v1alpha1.MicroVM) (*v1alpha1.MicroVM, error) {
	if vm.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if vm.Spec.Image == "" {
		return nil, fmt.Errorf("spec.image is required")
	}
	if vm.Spec.Runtime == "" {
		vm.Spec.Runtime = "firecracker"
	}

	k := key(vm.Namespace, vm.Name)
	s.mu.RLock()
	if _, exists := s.vms[k]; exists {
		s.mu.RUnlock()
		return nil, fmt.Errorf("microvm %q already exists", vm.Name)
	}
	s.mu.RUnlock()

	result, err := s.provider.Claim(ctx, provider.ClaimRequest{
		Image:     vm.Spec.Image,
		CPUMillis: vm.Spec.Resources.CPU,
		MemoryMiB: vm.Spec.Resources.MemoryMiB,
	})
	if err != nil {
		return nil, fmt.Errorf("provider claim: %w", err)
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

// GetVM returns a VM by namespace/name.
func (s *Storage) GetVM(namespace, name string) (*v1alpha1.MicroVM, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	vm, ok := s.vms[key(namespace, name)]
	if !ok {
		return nil, false
	}
	return vm.DeepCopy(), true
}

// ListVMs returns all VMs in a namespace (empty namespace = all).
func (s *Storage) ListVMs(namespace string) *v1alpha1.MicroVMList {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := &v1alpha1.MicroVMList{}
	for _, vm := range s.vms {
		if namespace != "" && vm.Namespace != namespace {
			continue
		}
		list.Items = append(list.Items, *vm.DeepCopy())
	}
	return list
}

// ListAll returns all VMs across all namespaces.
func (s *Storage) ListAll(ctx context.Context) (*v1alpha1.MicroVMList, error) {
	return s.ListVMs(""), nil
}

// DeleteVM releases the VM via the provider and removes it from storage.
func (s *Storage) DeleteVM(ctx context.Context, namespace, name string) error {
	k := key(namespace, name)

	s.mu.Lock()
	vm, ok := s.vms[k]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("microvm %q not found", name)
	}
	delete(s.vms, k)
	s.mu.Unlock()

	if err := s.provider.Release(ctx, vm.Status.VMID); err != nil {
		fmt.Printf("warn: provider release %s: %v\n", vm.Status.VMID, err)
	}
	return nil
}

// TrackVM adds a VM to storage (for branches).
func (s *Storage) TrackVM(vm *v1alpha1.MicroVM) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vms[key(vm.Namespace, vm.Name)] = vm.DeepCopy()
}
