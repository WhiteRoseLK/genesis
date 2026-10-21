// SPDX-License-Identifier: Apache-2.0

package proxmoxapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// StorageConfig represents a Proxmox storage pool configuration.
type StorageConfig struct {
	Storage string `json:"storage"`
	Type    string `json:"type"`
	Content string `json:"content"`
}

// GetStorage queries a storage pool's configuration.
func (c *Client) GetStorage(ctx context.Context, storage string) (*StorageConfig, error) {
	var s StorageConfig
	path := fmt.Sprintf("/storage/%s", url.PathEscape(storage))
	if err := c.do(ctx, http.MethodGet, path, nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// EnsureStorageContent ensures that the given storage pool has the required content type enabled.
// If the content type is not yet enabled, it updates the storage configuration via the Proxmox API.
func (c *Client) EnsureStorageContent(ctx context.Context, storage, requiredContent string) error {
	cfg, err := c.GetStorage(ctx, storage)
	if err != nil {
		return fmt.Errorf("getting storage %q configuration: %w", storage, err)
	}
	for _, cType := range strings.Split(cfg.Content, ",") {
		if strings.TrimSpace(cType) == requiredContent {
			return nil
		}
	}
	newContent := cfg.Content
	if newContent == "" {
		newContent = requiredContent
	} else {
		newContent = newContent + "," + requiredContent
	}
	form := url.Values{}
	form.Set("content", newContent)
	path := fmt.Sprintf("/storage/%s", url.PathEscape(storage))
	if err := c.do(ctx, http.MethodPut, path, form, nil); err != nil {
		return fmt.Errorf("updating storage %q content: %w", storage, err)
	}
	return nil
}

// StorageContent describes a file or volume within a Proxmox storage pool.
type StorageContent struct {
	VolID   string `json:"volid"`
	Content string `json:"content"`
	Format  string `json:"format"`
	Size    int64  `json:"size"`
}

// ListStorageContent lists all volumes/files in a storage pool on a given node.
func (c *Client) ListStorageContent(ctx context.Context, node, storage string) ([]StorageContent, error) {
	var list []StorageContent
	path := fmt.Sprintf("/nodes/%s/storage/%s/content", url.PathEscape(node), url.PathEscape(storage))
	if err := c.do(ctx, http.MethodGet, path, nil, &list); err != nil {
		return nil, err
	}
	return list, nil
}

// FindStorageVolume looks for an existing volume by its VolID (e.g. "local:import/ubuntu-24.04.qcow2").
func (c *Client) FindStorageVolume(ctx context.Context, node, storage, volID string) (*StorageContent, error) {
	list, err := c.ListStorageContent(ctx, node, storage)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].VolID == volID {
			return &list[i], nil
		}
	}
	return nil, nil
}

// DownloadURL instructs Proxmox to download a remote file directly into the storage pool.
// Returns the UPID of the asynchronous download task.
func (c *Client) DownloadURL(ctx context.Context, node, storage, downloadURL, filename, contentType string) (string, error) {
	form := url.Values{}
	form.Set("url", downloadURL)
	form.Set("filename", filename)
	form.Set("content", contentType)
	path := fmt.Sprintf("/nodes/%s/storage/%s/download-url", url.PathEscape(node), url.PathEscape(storage))
	var upid string
	if err := c.do(ctx, http.MethodPost, path, form, &upid); err != nil {
		return "", fmt.Errorf("initiating download of %s: %w", downloadURL, err)
	}
	return upid, nil
}
