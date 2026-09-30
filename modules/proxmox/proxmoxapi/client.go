// SPDX-License-Identifier: Apache-2.0

// Package proxmoxapi is a minimal client of the Proxmox VE REST API — just
// what the module needs (docs/07-mvp-modules.md), not a generic library.
// Written by hand rather than with a third-party SDK: the surface needed is
// narrow, and a third-party SDK would add a dependency that cannot be
// validated against a real cluster in this environment anyway
// (docs/PROGRESS.md, M5: "no Proxmox access for now").
package proxmoxapi

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TLSOptions controls TLS verification for communicating with the Proxmox API.
type TLSOptions struct {
	Insecure  bool
	CACertPEM []byte
}

// NewHTTPClient returns an *http.Client configured with the given TLS options.
// If neither Insecure nor CACertPEM is provided, it returns http.DefaultClient.
func NewHTTPClient(opts TLSOptions) (*http.Client, error) {
	if !opts.Insecure && len(opts.CACertPEM) == 0 {
		return http.DefaultClient, nil
	}
	tlsConfig := &tls.Config{
		InsecureSkipVerify: opts.Insecure, //nolint:gosec // G402: operator opt-in for self-signed certificates
	}
	if len(opts.CACertPEM) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(opts.CACertPEM) {
			return nil, fmt.Errorf("parsing CA certificate PEM: invalid or no PEM certificates found")
		}
		tlsConfig.RootCAs = pool
	}
	transport := &http.Transport{
		Proxy:           http.ProxyFromEnvironment,
		TLSClientConfig: tlsConfig,
	}
	return &http.Client{
		Transport: transport,
	}, nil
}

// Client talks to the REST API of a Proxmox VE cluster.
type Client struct {
	baseURL     string // ex. https://pve01.home.arpa:8006/api2/json
	tokenID     string
	tokenSecret string
	httpClient  *http.Client
}

// New builds a Client. A nil httpClient uses http.DefaultClient — pass a
// dedicated client in tests to point to a fixture server.
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

// apiError is the error returned by the Proxmox API (non-2xx status).
type apiError struct {
	StatusCode int
	Message    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("proxmox: %s (HTTP %d)", e.Message, e.StatusCode)
}

// envelope is the common shape of every Proxmox response: {"data": ...}.
type envelope struct {
	Data json.RawMessage `json:"data"`
}

// do runs a request and decodes its "data" envelope into out (nil to ignore
// the body).
func (c *Client) do(ctx context.Context, method, path string, form url.Values, out any) error {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("building the request %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", fmt.Sprintf("PVEAPIToken=%s=%s", c.tokenID, c.tokenSecret))
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading the response of %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(raw))}
	}

	if out == nil {
		return nil
	}
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("unexpected response from %s %s: %w\n%s", method, path, err, raw)
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decoding the response of %s %s: %w", method, path, err)
	}
	return nil
}

// TaskStatus is the state of an asynchronous Proxmox task (identified by its
// UPID).
type TaskStatus struct {
	Status     string `json:"status"`     // running | stopped
	ExitStatus string `json:"exitstatus"` // "OK" or an error message, once stopped
}

// WaitForTask polls /nodes/{node}/tasks/{upid}/status until the task ends, or
// ctx expires.
func (c *Client) WaitForTask(ctx context.Context, node, upid string) error {
	path := fmt.Sprintf("/nodes/%s/tasks/%s/status", node, url.PathEscape(upid))
	for {
		var status TaskStatus
		if err := c.do(ctx, http.MethodGet, path, nil, &status); err != nil {
			return fmt.Errorf("tracking task %s: %w", upid, err)
		}
		if status.Status == "stopped" {
			if status.ExitStatus != "OK" {
				return fmt.Errorf("task %s failed: %s", upid, status.ExitStatus)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("task %s: %w", upid, ctx.Err())
		case <-time.After(time.Second):
		}
	}
}

// Version checks access to the API (docs/05-bootstrap-lifecycle.md, Phase 0:
// Validate).
func (c *Client) Version(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	if err := c.do(ctx, http.MethodGet, "/version", nil, &v); err != nil {
		return "", err
	}
	return v.Version, nil
}
