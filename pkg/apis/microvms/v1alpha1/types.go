package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MicroVM is a lightweight virtual machine managed by a runtime provider.
type MicroVM struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   MicroVMSpec   `json:"spec,omitempty"`
	Status MicroVMStatus `json:"status,omitempty"`
}

type MicroVMSpec struct {
	// Image is the rootfs image (e.g., "python", "node").
	Image string `json:"image"`

	// Runtime selects the VM provider (e.g., "firecracker").
	// +optional
	Runtime string `json:"runtime,omitempty"`

	// Resources specifies CPU and memory for the VM.
	// +optional
	Resources MicroVMResources `json:"resources,omitempty"`
}

type MicroVMResources struct {
	// CPU in millicores (e.g., 500 = half core, 1000 = full core).
	// +optional
	CPU int `json:"cpu,omitempty"`

	// Memory in MiB.
	// +optional
	MemoryMiB int `json:"memoryMiB,omitempty"`
}

type MicroVMStatus struct {
	// Phase is the current lifecycle phase.
	Phase MicroVMPhase `json:"phase,omitempty"`

	// NodeName is the agent node hosting this VM.
	NodeName string `json:"nodeName,omitempty"`

	// VMID is the agent-side VM identifier.
	VMID string `json:"vmID,omitempty"`

	// IP is the guest's network address.
	IP string `json:"ip,omitempty"`

	// SSH holds SSH connection info if enabled.
	// +optional
	SSH *SSHStatus `json:"ssh,omitempty"`
}

type SSHStatus struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host,omitempty"`
	Port    int    `json:"port,omitempty"`
}

type MicroVMPhase string

const (
	MicroVMPending     MicroVMPhase = "Pending"
	MicroVMRunning     MicroVMPhase = "Running"
	MicroVMTerminated  MicroVMPhase = "Terminated"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MicroVMList is a list of MicroVM resources.
type MicroVMList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []MicroVM `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MicroVMExecOptions describes the options for exec on a MicroVM.
type MicroVMExecOptions struct {
	metav1.TypeMeta `json:",inline"`

	// Command to execute inside the VM.
	Command []string `json:"command"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MicroVMBranchOptions describes the options for branching a MicroVM.
type MicroVMBranchOptions struct {
	metav1.TypeMeta `json:",inline"`

	// Count is the number of branches to create.
	Count int `json:"count"`
}
