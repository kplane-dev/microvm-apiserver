// Package provider defines the runtime provider interface for MicroVM lifecycle.
package provider

import "context"

// ExecResult holds the output of a command executed inside a VM.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// VMStatus holds the current state of a VM from the agent's perspective.
type VMStatus struct {
	VMID     string
	NodeIP   string
	GuestIP  string
	Image    string
	Running  bool
}

// ClaimRequest describes the desired VM configuration.
type ClaimRequest struct {
	Image     string
	CPUMillis int
	MemoryMiB int
}

// BranchResult holds info about a single branch VM.
type BranchResult struct {
	VMID    string
	GuestIP string
}

// RuntimeProvider is the interface that runtime backends implement.
// The first implementation is Firecracker; future runtimes (Unikraft, QEMU)
// implement the same interface.
type RuntimeProvider interface {
	// Claim provisions a new VM and returns its status.
	Claim(ctx context.Context, req ClaimRequest) (*VMStatus, error)

	// Release destroys a VM.
	Release(ctx context.Context, vmID string) error

	// Exec runs a command inside a VM.
	Exec(ctx context.Context, vmID string, cmd []string) (*ExecResult, error)

	// Branch forks a running VM into N independent copies.
	Branch(ctx context.Context, vmID string, count int) ([]BranchResult, error)

	// EnableSSH starts the SSH daemon and injects the authorized key.
	EnableSSH(ctx context.Context, vmID string, publicKey string) (ip string, port int, err error)

	// DisableSSH stops the SSH daemon.
	DisableSSH(ctx context.Context, vmID string) error

	// List returns all active VMs.
	List(ctx context.Context) ([]VMStatus, error)

	// AgentStatus returns aggregate capacity across all agents.
	AgentStatus(ctx context.Context) (*AgentStatus, error)
}

// AgentStatus is the aggregate capacity across runtime agents.
type AgentStatus struct {
	Agents     int            // number of healthy agents
	Warm       map[string]int // image -> warm count
	Active     int            // total active VMs
	MaxPerNode int            // max VMs per node
}
