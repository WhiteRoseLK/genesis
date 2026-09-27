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

// NextID asks the cluster for a free VM ID.
func (c *Client) NextID(ctx context.Context) (int, error) {
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

// CloneVMOptions describes a linked clone (docs/07: "linked clone of the
// template").
type CloneVMOptions struct {
	TemplateID int
	NewID      int
	Name       string
}

// CloneVM makes a (linked) clone of the template into a new VM.
func (c *Client) CloneVM(ctx context.Context, node string, opts CloneVMOptions) error {
	form := url.Values{}
	form.Set("newid", strconv.Itoa(opts.NewID))
	form.Set("name", opts.Name)
	form.Set("full", "0")
	path := fmt.Sprintf("/nodes/%s/qemu/%d/clone", node, opts.TemplateID)
	return c.doMaybeAsync(ctx, node, http.MethodPost, path, form)
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
		// API).
		form.Set("sshkeys", url.QueryEscape(opts.SSHPublicKey))
	}
	if opts.IP != "" {
		val := "ip=" + opts.IP
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

// DeleteVM deletes a VM.
func (c *Client) DeleteVM(ctx context.Context, node string, vmid int) error {
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
