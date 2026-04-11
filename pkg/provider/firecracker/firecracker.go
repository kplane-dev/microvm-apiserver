// Package firecracker implements the RuntimeProvider for Firecracker agents.
package firecracker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/kplane-dev/microvm-apiserver/pkg/provider"
	"k8s.io/klog/v2"
)

// Provider implements provider.RuntimeProvider by talking to firecracker-agent
// instances discovered via Kubernetes endpoints.
type Provider struct {
	mu     sync.RWMutex
	agents []string // agent base URLs (e.g., "http://10.50.0.46:8090")

	// vmToAgent tracks which agent owns which VM
	vmAgent map[string]string // vmID -> agent URL
}

// New creates a new Firecracker provider.
func New() *Provider {
	return &Provider{
		vmAgent: make(map[string]string),
	}
}

// SetAgents updates the list of known agent URLs.
func (p *Provider) SetAgents(agents []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agents = agents
	klog.Infof("firecracker provider: %d agents", len(agents))
}

func (p *Provider) Claim(ctx context.Context, req provider.ClaimRequest) (*provider.VMStatus, error) {
	p.mu.RLock()
	if len(p.agents) == 0 {
		p.mu.RUnlock()
		return nil, fmt.Errorf("no firecracker agents available")
	}

	// Simple scheduling: pick the first agent (TODO: least-loaded)
	agent := p.agents[0]
	p.mu.RUnlock()

	params := url.Values{
		"image": {req.Image},
	}
	if req.CPUMillis > 0 {
		params.Set("cpu", fmt.Sprintf("%d", req.CPUMillis))
	}
	if req.MemoryMiB > 0 {
		params.Set("memory", fmt.Sprintf("%d", req.MemoryMiB))
	}

	var resp struct {
		VMID       string `json:"vm_id"`
		Image      string `json:"image"`
		MemoryMiB  int    `json:"memory_mib"`
		CPUMillis  int    `json:"cpu_millicores"`
		FromWarm   bool   `json:"from_warm_pool"`
	}
	if err := p.agentGet(ctx, agent, "/claim", params, &resp); err != nil {
		return nil, fmt.Errorf("claim from %s: %w", agent, err)
	}

	p.mu.Lock()
	p.vmAgent[resp.VMID] = agent
	p.mu.Unlock()

	return &provider.VMStatus{
		VMID:    resp.VMID,
		NodeIP:  agent,
		Image:   resp.Image,
		Running: true,
	}, nil
}

func (p *Provider) Release(ctx context.Context, vmID string) error {
	agent, err := p.agentForVM(vmID)
	if err != nil {
		return err
	}

	if err := p.agentGet(ctx, agent, "/release", url.Values{"vm_id": {vmID}}, nil); err != nil {
		return fmt.Errorf("release %s: %w", vmID, err)
	}

	p.mu.Lock()
	delete(p.vmAgent, vmID)
	p.mu.Unlock()
	return nil
}

func (p *Provider) Exec(ctx context.Context, vmID string, cmd []string) (*provider.ExecResult, error) {
	agent, err := p.agentForVM(vmID)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Stdout   string `json:"stdout"`
		Stderr   string `json:"stderr"`
		ExitCode int    `json:"exit_code"`
	}
	// The agent's /exec wraps in /bin/sh -c, so send the raw command.
	// If caller passes ["/bin/sh", "-c", "node -v"], extract the actual command.
	cmdStr := strings.Join(cmd, " ")
	if len(cmd) >= 3 && (cmd[0] == "/bin/sh" || cmd[0] == "sh") && cmd[1] == "-c" {
		cmdStr = strings.Join(cmd[2:], " ")
	}
	params := url.Values{
		"vm_id": {vmID},
		"cmd":   {cmdStr},
	}
	if err := p.agentGet(ctx, agent, "/exec", params, &resp); err != nil {
		return nil, fmt.Errorf("exec %s: %w", vmID, err)
	}

	return &provider.ExecResult{
		Stdout:   resp.Stdout,
		Stderr:   resp.Stderr,
		ExitCode: resp.ExitCode,
	}, nil
}

func (p *Provider) Branch(ctx context.Context, vmID string, count int) ([]provider.BranchResult, error) {
	agent, err := p.agentForVM(vmID)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Branches []struct {
			VMID string `json:"vm_id"`
			PID  int    `json:"pid"`
		} `json:"branches"`
		TotalTimeMs int `json:"total_time_ms"`
	}
	params := url.Values{
		"vm_id": {vmID},
		"count": {fmt.Sprintf("%d", count)},
	}
	if err := p.agentGet(ctx, agent, "/branch", params, &resp); err != nil {
		return nil, fmt.Errorf("branch %s: %w", vmID, err)
	}

	results := make([]provider.BranchResult, len(resp.Branches))
	p.mu.Lock()
	for i, b := range resp.Branches {
		results[i] = provider.BranchResult{VMID: b.VMID}
		p.vmAgent[b.VMID] = agent // branches are on the same agent
	}
	p.mu.Unlock()

	return results, nil
}

func (p *Provider) EnableSSH(ctx context.Context, vmID string, publicKey string) (string, int, error) {
	agent, err := p.agentForVM(vmID)
	if err != nil {
		return "", 0, err
	}

	var resp struct {
		SSHIP   string `json:"ssh_ip"`
		SSHPort int    `json:"ssh_port"`
		Status  string `json:"status"`
	}
	params := url.Values{
		"vm_id": {vmID},
		"key":   {publicKey},
	}
	if err := p.agentGet(ctx, agent, "/ssh/enable", params, &resp); err != nil {
		return "", 0, fmt.Errorf("enable ssh %s: %w", vmID, err)
	}
	return resp.SSHIP, resp.SSHPort, nil
}

func (p *Provider) DisableSSH(ctx context.Context, vmID string) error {
	agent, err := p.agentForVM(vmID)
	if err != nil {
		return err
	}
	return p.agentGet(ctx, agent, "/ssh/disable", url.Values{"vm_id": {vmID}}, nil)
}

func (p *Provider) List(ctx context.Context) ([]provider.VMStatus, error) {
	// TODO: poll /list on each agent
	return nil, nil
}

func (p *Provider) AgentStatus(ctx context.Context) (*provider.AgentStatus, error) {
	p.mu.RLock()
	agents := append([]string{}, p.agents...)
	p.mu.RUnlock()

	status := &provider.AgentStatus{
		Agents: len(agents),
		Warm:   make(map[string]int),
	}

	for _, agent := range agents {
		var resp struct {
			Warm   map[string]int `json:"warm"`
			Active int            `json:"active"`
			Max    int            `json:"max"`
		}
		if err := p.agentGet(ctx, agent, "/status", nil, &resp); err != nil {
			klog.Warningf("agent %s status failed: %v", agent, err)
			continue
		}
		for img, count := range resp.Warm {
			status.Warm[img] += count
		}
		status.Active += resp.Active
		status.MaxPerNode = resp.Max
	}
	return status, nil
}

func (p *Provider) agentForVM(vmID string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	agent, ok := p.vmAgent[vmID]
	if !ok {
		return "", fmt.Errorf("VM %s not found", vmID)
	}
	return agent, nil
}

func (p *Provider) agentGet(ctx context.Context, agent, path string, params url.Values, dest interface{}) error {
	u := agent + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("agent returned %d: %s", resp.StatusCode, string(body))
	}

	if dest != nil && len(body) > 0 {
		return json.Unmarshal(body, dest)
	}
	return nil
}
