// SPDX-License-Identifier: Apache-2.0

// Package proxmoxapi est un client minimal de l'API REST Proxmox VE — juste
// ce dont le module a besoin (docs/07-mvp-modules.md), pas une bibliothèque
// générique. Écrit à la main plutôt qu'avec un SDK tiers : la surface
// nécessaire est étroite, et un SDK tiers ajouterait une dépendance qu'on ne
// peut de toute façon pas valider contre un vrai cluster dans cet
// environnement (docs/PROGRESS.md, J5 : "pas d'accès Proxmox pour
// l'instant").
package proxmoxapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client parle à l'API REST d'un cluster Proxmox VE.
type Client struct {
	baseURL     string // ex. https://pve01.home.arpa:8006/api2/json
	tokenID     string
	tokenSecret string
	httpClient  *http.Client
}

// New construit un Client. httpClient nil utilise http.DefaultClient — passer
// un client dédié en test pour pointer vers un serveur de fixtures.
func New(endpoint, tokenID, tokenSecret string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{
		baseURL:     strings.TrimRight(endpoint, "/") + "/api2/json",
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		httpClient:  httpClient,
	}
}

// apiError est l'erreur renvoyée par l'API Proxmox (statut non-2xx).
type apiError struct {
	StatusCode int
	Message    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("proxmox : %s (HTTP %d)", e.Message, e.StatusCode)
}

// envelope est la forme commune de toutes les réponses Proxmox : {"data": ...}.
type envelope struct {
	Data json.RawMessage `json:"data"`
}

// do exécute une requête et décode son enveloppe "data" dans out (nil pour
// ignorer le corps).
func (c *Client) do(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("construction de la requête %s %s : %w", method, path, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", c.tokenID, c.tokenSecret))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s : %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("lecture de la réponse de %s %s : %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
	}

	if out == nil {
		return nil
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("réponse inattendue de %s %s : %w\n%s", method, path, err, raw)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("décodage de la réponse de %s %s : %w", method, path, err)
	}
	return nil
}

// TaskStatus est l'état d'une tâche asynchrone Proxmox (identifiée par UPID).
type TaskStatus struct {
	Status     string `json:"status"`     // running | stopped
	ExitStatus string `json:"exitstatus"` // "OK" ou un message d'erreur, une fois stopped
}

// WaitForTask sonde /nodes/{node}/tasks/{upid}/status jusqu'à ce que la
// tâche se termine, ou que ctx expire.
func (c *Client) WaitForTask(ctx context.Context, node, upid string) error {
	path := fmt.Sprintf("/nodes/%s/tasks/%s/status", node, url.PathEscape(upid))
	for {
		var status TaskStatus
		if err := c.do(ctx, http.MethodGet, path, nil, &status); err != nil {
			return fmt.Errorf("suivi de la tâche %s : %w", upid, err)
		}
		if status.Status == "stopped" {
			if status.ExitStatus != "OK" {
				return fmt.Errorf("tâche %s en échec : %s", upid, status.ExitStatus)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("tâche %s : %w", upid, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// Version vérifie l'accès à l'API (docs/05-bootstrap-lifecycle.md, Phase 0 : Validate).
func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, http.MethodGet, "/version", nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}
