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

// pdnsClient parle directement à l'API REST de PowerDNS Authoritative
// (docs/07-mvp-modules.md : "Authoritative (SQLite, API)"), à la manière de
// modules/proxmox/proxmoxapi pour Proxmox — un module appelle l'API du
// produit qu'il pilote directement, pas via ansible, quand le produit en
// expose une.
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

// fqdn ajoute le point final PowerDNS attend sur tout nom pleinement
// qualifié (zones et enregistrements).
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
			return 0, nil, fmt.Errorf("encodage de la requête PowerDNS : %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return 0, nil, fmt.Errorf("construction de la requête PowerDNS : %w", err)
	}
	req.Header.Set("X-API-Key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("requête PowerDNS %s %s : %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("lecture de la réponse PowerDNS %s %s : %w", method, path, err)
	}
	return resp.StatusCode, data, nil
}

// ensureZone crée la zone si elle n'existe pas encore — idempotent : 201
// (créée) et 409/422 (déjà existante, réponses PowerDNS observées selon
// version) sont tous les deux un succès.
func (c *pdnsClient) ensureZone(ctx context.Context, zone string) error {
	zoneName := fqdn(zone)
	status, body, err := c.request(ctx, http.MethodPost, "/api/v1/servers/localhost/zones", pdnsZoneCreate{
		Name: zoneName, Kind: "Native", Nameservers: []string{"ns1." + zoneName},
	})
	if err != nil {
		return fmt.Errorf("création de la zone %q : %w", zone, err)
	}
	switch status {
	case http.StatusCreated, http.StatusConflict, http.StatusUnprocessableEntity:
		return nil
	default:
		return fmt.Errorf("création de la zone %q : PowerDNS a répondu %d : %s", zone, status, body)
	}
}

func (c *pdnsClient) patchRRset(ctx context.Context, zone string, rrset pdnsRRset) error {
	status, body, err := c.request(ctx, http.MethodPatch, "/api/v1/servers/localhost/zones/"+url.PathEscape(fqdn(zone)), pdnsZonePatch{
		RRsets: []pdnsRRset{rrset},
	})
	if err != nil {
		return fmt.Errorf("mise à jour de %s %s dans %q : %w", rrset.Type, rrset.Name, zone, err)
	}
	if status != http.StatusNoContent && status != http.StatusOK {
		return fmt.Errorf("mise à jour de %s %s dans %q : PowerDNS a répondu %d : %s", rrset.Type, rrset.Name, zone, status, body)
	}
	return nil
}

func (c *pdnsClient) listRRsets(ctx context.Context, zone string) ([]pdnsRRset, error) {
	status, body, err := c.request(ctx, http.MethodGet, "/api/v1/servers/localhost/zones/"+url.PathEscape(fqdn(zone)), nil)
	if err != nil {
		return nil, fmt.Errorf("lecture de la zone %q : %w", zone, err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("lecture de la zone %q : PowerDNS a répondu %d : %s", zone, status, body)
	}
	var zoneResp pdnsZoneResponse
	if err := json.Unmarshal(body, &zoneResp); err != nil {
		return nil, fmt.Errorf("décodage de la zone %q : %w", zone, err)
	}
	return zoneResp.RRsets, nil
}
