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

// VM est le sous-ensemble de champs d'une VM Proxmox dont le module a besoin.
type VM struct {
	VMID     int    `json:"vmid"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Template int    `json:"template,omitempty"` // 1 si c'est un template
}

// NextID demande au cluster un identifiant de VM libre.
func (c *Client) NextID(ctx context.Context) (int, error) {
	var idStr string
	if err := c.do(ctx, http.MethodGet, "/cluster/nextid", nil, &idStr); err != nil {
		return 0, err
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return 0, fmt.Errorf("identifiant inattendu depuis /cluster/nextid : %q", idStr)
	}
	return id, nil
}

// ListVMs liste les VM (et templates) du nœud.
func (c *Client) ListVMs(ctx context.Context, node string) ([]VM, error) {
	var vms []VM
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu", node), nil, &vms); err != nil {
		return nil, err
	}
	return vms, nil
}

// FindVMByName cherche une VM existante par nom — clé d'idempotence
// d'EnsureVM avec le tag genesis-env (docs/07-mvp-modules.md).
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

// FindTemplateByName cherche un template existant par nom.
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

// doMaybeAsync exécute une opération dont la réponse est soit un UPID à
// attendre (VM en cours d'exécution : changement différé), soit vide
// (VM arrêtée : appliqué de façon synchrone) — les deux existent réellement
// selon l'état de la VM au moment de l'appel.
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

// CloneVMOptions décrit un clone lié (docs/07 : "clone lié du template").
type CloneVMOptions struct {
	TemplateID int
	NewID      int
	Name       string
}

// CloneVM clone (lié) le template vers une nouvelle VM.
func (c *Client) CloneVM(ctx context.Context, node string, opts CloneVMOptions) error {
	form := url.Values{}
	form.Set("newid", strconv.Itoa(opts.NewID))
	form.Set("name", opts.Name)
	form.Set("full", "0")
	path := fmt.Sprintf("/nodes/%s/qemu/%d/clone", node, opts.TemplateID)
	return c.doMaybeAsync(ctx, node, http.MethodPost, path, form)
}

// CloudInitOptions configure cloud-init sur une VM (docs/07 : "IP, clé SSH
// de service, utilisateur genesis").
type CloudInitOptions struct {
	User         string
	SSHPublicKey string
	IP           string // vide = dhcp
	Gateway      string
	Tags         []string
}

// ConfigureCloudInit applique la configuration cloud-init.
func (c *Client) ConfigureCloudInit(ctx context.Context, node string, vmid int, opts CloudInitOptions) error {
	form := url.Values{}
	if opts.User != "" {
		form.Set("ciuser", opts.User)
	}
	if opts.SSHPublicKey != "" {
		// L'API Proxmox attend sshkeys ré-encodé en URL avant l'encodage de
		// formulaire lui-même (double encodage, quirk documenté de l'API).
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

// StartVM démarre une VM.
func (c *Client) StartVM(ctx context.Context, node string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/start", node, vmid)
	return c.doMaybeAsync(ctx, node, http.MethodPost, path, nil)
}

// DeleteVM supprime une VM.
func (c *Client) DeleteVM(ctx context.Context, node string, vmid int) error {
	path := fmt.Sprintf("/nodes/%s/qemu/%d", node, vmid)
	return c.doMaybeAsync(ctx, node, http.MethodDelete, path, nil)
}

// VMStatus est le statut courant d'une VM.
type VMStatus struct {
	VMID   int    `json:"vmid"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Status interroge le statut courant d'une VM.
func (c *Client) Status(ctx context.Context, node string, vmid int) (*VMStatus, error) {
	var s VMStatus
	path := fmt.Sprintf("/nodes/%s/qemu/%d/status/current", node, vmid)
	if err := c.do(ctx, http.MethodGet, path, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// NodeTime est l'heure du nœud (docs/05-bootstrap-lifecycle.md : contrôle d'horloge).
type NodeTime struct {
	Time int64 `json:"time"` // secondes Unix
}

// Time interroge l'heure du nœud.
func (c *Client) Time(ctx context.Context, node string) (*NodeTime, error) {
	var t NodeTime
	if err := c.do(ctx, http.MethodGet, fmt.Sprintf("/nodes/%s/time", node), nil, &t); err != nil {
		return nil, err
	}
	return &t, nil
}
