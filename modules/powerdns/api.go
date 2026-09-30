// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// pdnsClient talks directly to the PowerDNS Authoritative REST API
// (docs/07-mvp-modules.md: "Authoritative (SQLite, API)"), in the manner of
// modules/proxmox/proxmoxapi for Proxmox — a module calls the API of the
// product it drives directly, not through ansible, when the product exposes
// one.
type pdnsClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func newPDNSClient(baseURL, apiKey string) *pdnsClient {
	return &pdnsClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

type pdnsRecordContent struct {
	Content  string `json:"content"`
	Disabled bool   `json:"disabled"`
}

type pdnsRRset struct {
	Name       string              `json:"name"`
	Type       string              `json:"type"`
	TTL        uint32              `json:"ttl,omitempty"`
	ChangeType string              `json:"changetype"`
	Records    []pdnsRecordContent `json:"records,omitempty"`
}

type pdnsZonePatch struct {
	RRsets []pdnsRRset `json:"rrsets"`
}

type pdnsZoneCreate struct {
	Name        string   `json:"name"`
	Kind        string   `json:"kind"`
	Nameservers []string `json:"nameservers"`
}

type pdnsZoneResponse struct {
	RRsets []pdnsRRset `json:"rrsets"`
}

// fqdn adds the trailing dot PowerDNS expects on every fully qualified name
// (zones and records).
func fqdn(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

func (c *pdnsClient) request(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, fmt.Errorf("encoding the PowerDNS request: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("building the PowerDNS request: %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("PowerDNS request %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("reading the PowerDNS response %s %s: %w", method, path, err)
	}
	return resp.StatusCode, data, nil
}

// ensureZone creates the zone if it does not exist yet — idempotent: 201
// (created) and 409/422 (already exists, PowerDNS answers observed depending
// on the version) are both a success.
func (c *pdnsClient) ensureZone(ctx context.Context, zone string) error {
	zoneName := fqdn(zone)
	status, body, err := c.request(ctx, http.MethodPost, "/api/v1/servers/localhost/zones", pdnsZoneCreate{
		Name: zoneName, Kind: "Native", Nameservers: []string{"ns1." + zoneName},
	})
	if err != nil {
		return fmt.Errorf("creating zone %q: %w", zone, err)
	}
	switch status {
	case http.StatusCreated, http.StatusConflict, http.StatusUnprocessableEntity:
		return nil
	default:
		return fmt.Errorf("creating zone %q: PowerDNS answered %d: %s", zone, status, body)
	}
}

func (c *pdnsClient) patchRRset(ctx context.Context, zone string, rrset pdnsRRset) error {
	status, body, err := c.request(ctx, http.MethodPatch, "/api/v1/servers/localhost/zones/"+url.PathEscape(fqdn(zone)), pdnsZonePatch{
		RRsets: []pdnsRRset{rrset},
	})
	if err != nil {
		return fmt.Errorf("updating %s %s in %q: %w", rrset.Type, rrset.Name, zone, err)
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("updating %s %s in %q: PowerDNS answered %d: %s", rrset.Type, rrset.Name, zone, status, body)
	}
	return nil
}

func (c *pdnsClient) listRRsets(ctx context.Context, zone string) ([]pdnsRRset, error) {
	status, body, err := c.request(ctx, http.MethodGet, "/api/v1/servers/localhost/zones/"+url.PathEscape(fqdn(zone)), nil)
	if err != nil {
		return nil, fmt.Errorf("reading zone %q: %w", zone, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("reading zone %q: PowerDNS answered %d: %s", zone, status, body)
	}
	var zoneResp pdnsZoneResponse
	if err := json.Unmarshal(body, &zoneResp); err != nil {
		return nil, fmt.Errorf("decoding zone %q: %w", zone, err)
	}
	return zoneResp.RRsets, nil
}
