// SPDX-License-Identifier: Apache-2.0

package proxmoxapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// VM is the subset of a Proxmox VM's fields that the module needs.
type VM struct {
	VMID     int    `json:"vmid"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Template int    `json:"template,omitempty"` // 1 if it is a template
}

// NextID asks the cluster for a free VM ID. If minID is provided and > 0,
// it finds the first free ID starting from minID.
func (c *Client) NextID(ctx context.Context, minID ...int) (int, error) {
	start := 0
	if len(minID) > 0 {
		start = minID[0]
	}
	if start <= 0 {
		var idStr string
		if err := c.do(ctx, http.MethodGet, "/cluster/nextid", nil, &idStr); err != nil {
			return 0, err
		}
		id, err := strconv.Atoi(idStr)
		if err != nil {
			return 0, fmt.Errorf("unexpected ID from /cluster/nextid: %q", idStr)
		}
		return id, nil
	}

	// Proxmox /cluster/nextid?vmid=X checks if X is available. If X already
	// exists, it returns HTTP 400 "VM X already exists". We find the first free
	// ID starting from start.
	for candidate := start; candidate < start+10000; candidate++ {
		var idStr string
		err := c.do(ctx, http.MethodGet, fmt.Sprintf("/cluster/nextid?vmid=%d", candidate), nil, &idStr)
		if err == nil {
			id, err := strconv.Atoi(idStr)
			if err != nil {
				return 0, fmt.Errorf("unexpected ID from /cluster/nextid: %q", idStr)
			}
			return id, nil
		}
		if !strings.Contains(err.Error(), "already exists") {
			return 0, err
		}
	}
	return 0, fmt.Errorf("no free VM ID found starting from %d", start)
}

// ListVMs lists the node's VMs (and templates).
func (c *Client) ListVMs(ctx context.Context, node string) ([]VM, error) {
	var vms []VM
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu", node), nil, &vms); err != nil {
		return nil, err
	}
	return vms, nil
}

// FindVMByName looks for an existing VM by name — EnsureVM's idempotence key,
// with the genesis-env tag (docs/07-mvp-modules.md).
func (c *Client) FindVMByName(ctx context.Context, node, name string) (*VM, error) {
	vms, err := c.ListVMs(ctx, node)
	if err != nil {
		return nil, err
	}
	for i := range vms {
		if vms[i].Name == name && vms[i].Template == 0 {
			return &vms[i], nil
		}
	}
	return nil, nil
}

// FindTemplateByName looks for an existing template by name.
func (c *Client) FindTemplateByName(ctx context.Context, node, name string) (*VM, error) {
	vms, err := c.ListVMs(ctx, node)
	if err != nil {
		return nil, err
	}
	for i := range vms {
		if vms[i].Name == name && vms[i].Template == 1 {
			return &vms[i], nil
		}
	}
	return nil, nil
}

// doMaybeAsync runs an operation whose response is either a UPID to wait for
// (running VM: deferred change) or empty (stopped VM: applied synchronously) —
// both really happen, depending on the VM's state at the time of the call.
func (c *Client) doMaybeAsync(ctx context.Context, node, method, path string, form url.Values) error {
	var raw json.RawMessage
	if err := c.do(ctx, method, path, form, &raw); err != nil {
		return err
	}
	var upid string
	if err := json.Unmarshal(raw, &upid); err == nil && upid != "" {
		return c.WaitForTask(ctx, node, upid)
	}
	return nil
}

// CloneVMOptions describes a clone of a template.
type CloneVMOptions struct {
	TemplateID int
	NewID      int
	Name       string
	Full       bool
	Storage    string
}

// CloneVM makes a clone of the template into a new VM.
func (c *Client) CloneVM(ctx context.Context, node string, opts CloneVMOptions) error {
	form := url.Values{}
	form.Set("newid", strconv.Itoa(opts.NewID))
	form.Set("name", opts.Name)
	if opts.Full {
		form.Set("full", "1")
	} else {
		form.Set("full", "0")
	}
	if opts.Storage != "" {
		form.Set("storage", opts.Storage)
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/clone", node, opts.TemplateID)
	err := c.doMaybeAsync(ctx, node, http.MethodPost, path, form)
	if err != nil && !opts.Full && strings.Contains(err.Error(), "Linked clone feature is not supported") {
		// Fallback to full clone if linked clone is unsupported on this storage (e.g. standard LVM).
		form.Set("full", "1")
		return c.doMaybeAsync(ctx, node, http.MethodPost, path, form)
	}
	return err
}

// CloudInitOptions configures cloud-init on a VM (docs/07: "IP, service SSH
// key, genesis user").
type CloudInitOptions struct {
	User         string
	SSHPublicKey string
	IP           string // empty = dhcp
	Gateway      string
	Tags         []string
}

// ConfigureCloudInit applies the cloud-init configuration.
func (c *Client) ConfigureCloudInit(ctx context.Context, node string, vmid int, opts CloudInitOptions) error {
	form := url.Values{}
	if opts.User != "" {
		form.Set("ciuser", opts.User)
	}
	if opts.SSHPublicKey != "" {
		// The Proxmox API expects sshkeys to be URL-encoded again before the
		// form encoding itself (double encoding, a documented quirk of the
		// API). Proxmox requires %20 for spaces rather than '+' from QueryEscape.
		escaped := strings.ReplaceAll(url.QueryEscape(strings.TrimSpace(opts.SSHPublicKey)), "+", "%20")
		form.Set("sshkeys", escaped)
	}
	if opts.IP != "" {
		ip := opts.IP
		if !strings.Contains(ip, "/") {
			ip += "/24"
		}
		val := "ip=" + ip
		if opts.Gateway != "" {
			val += ",gw=" + opts.Gateway
		}
		form.Set("ipconfig0", val)
	} else {
		form.Set("ipconfig0", "ip=dhcp")
	}
	if len(opts.Tags) > 0 {
		form.Set("tags", strings.Join(opts.Tags, ";"))
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d/config", node, vmid)
	return c.doMaybeAsync(ctx, node, http.MethodPut, path, form)
}

// StartVM starts a VM.
func (c *Client) StartVM(ctx context.Context, node string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/start", node, vmid)
	return c.doMaybeAsync(ctx, node, http.MethodPost, path, nil)
}

// StopVM stops a running VM immediately.
func (c *Client) StopVM(ctx context.Context, node string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/stop", node, vmid)
	return c.doMaybeAsync(ctx, node, http.MethodPost, path, nil)
}

// DeleteVM deletes a VM. If the VM is running, it stops it first.
func (c *Client) DeleteVM(ctx context.Context, node string, vmid int) error {
	status, err := c.Status(ctx, node, vmid)
	if err == nil && status != nil && status.Status == "running" {
		if stopErr := c.StopVM(ctx, node, vmid); stopErr != nil {
			return fmt.Errorf("stopping VM %d before deletion: %w", vmid, stopErr)
		}
	}
	path := fmt.Sprintf("/nodes/%s/qemu/%d", node, vmid)
	return c.doMaybeAsync(ctx, node, http.MethodDelete, path, nil)
}

// VMStatus is a VM's current status.
type VMStatus struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Status queries a VM's current status.
func (c *Client) Status(ctx context.Context, node string, vmid int) (*VMStatus, error) {
	var s VMStatus
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/current", node, vmid)
	if err := c.do(ctx, http.MethodGet, path, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// NodeTime is the node's clock (docs/05-bootstrap-lifecycle.md: clock check).
type NodeTime struct {
	Time int64 `json:"time"` // Unix seconds
}

// Time queries the node's clock.
func (c *Client) Time(ctx context.Context, node string) (*NodeTime, error) {
	var t NodeTime
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/time", node), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
