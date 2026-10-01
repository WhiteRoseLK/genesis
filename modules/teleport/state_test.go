// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

func TestTargetFromState(t *testing.T) {
	pair := sshKeyPair{
		PrivateKeyOpenSSH:   "direct-private-key",
		PublicKeyAuthorized: "direct-public-key",
	}

	t.Run("default vm fields", func(t *testing.T) {
		st, err := sdk.NewState(map[string]any{
			"vm_ip":       "10.0.0.15",
			"vm_ssh_port": int64(22),
		})
		if err != nil {
			t.Fatalf("NewState: %v", err)
		}
		target := targetFromState(&modulev1.StepRequest{State: st}, pair)
		if target.Host != "10.0.0.15" {
			t.Errorf("Host = %q, want \"10.0.0.15\"", target.Host)
		}
		if target.Port != 22 {
			t.Errorf("Port = %d, want 22", target.Port)
		}
		if target.User != sshUser {
			t.Errorf("User = %q, want %q", target.User, sshUser)
		}
		if target.PrivateKey != "direct-private-key" {
			t.Errorf("PrivateKey = %q, want \"direct-private-key\"", target.PrivateKey)
		}
	})

	t.Run("persisted own_target fields", func(t *testing.T) {
		st, err := sdk.NewState(map[string]any{
			"own_target_host": "10.0.0.20",
			"own_target_port": int64(2222),
			"own_target_user": "custom-user",
		})
		if err != nil {
			t.Fatalf("NewState: %v", err)
		}
		target := targetFromState(&modulev1.StepRequest{State: st}, pair)
		if target.Host != "10.0.0.20" {
			t.Errorf("Host = %q, want \"10.0.0.20\"", target.Host)
		}
		if target.Port != 2222 {
			t.Errorf("Port = %d, want 2222", target.Port)
		}
		if target.User != "custom-user" {
			t.Errorf("User = %q, want \"custom-user\"", target.User)
		}
	})
}

func TestCheckRestoresState(t *testing.T) {
	ctx := context.Background()
	m := &teleportModule{}

	st, err := sdk.NewState(map[string]any{
		"vm_name":      "teleport-test",
		"vm_ip":        "10.0.0.50",
		"vm_ssh_port":  int64(22),
		"cluster_name": "teleport.example.com",
		"ca_pin":       "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"verified":     true,
	})
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}

	req := &modulev1.StepRequest{
		RunId: "test-run",
		State: st,
	}

	res, err := m.Check(ctx, req)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if res.GetStatus() != modulev1.CheckResult_STATUS_COMPLIANT {
		t.Errorf("Check status = %v, want COMPLIANT", res.GetStatus())
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.caPin != "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Errorf("caPin = %q, want sha256:...", m.caPin)
	}
	if m.clusterName != "teleport.example.com" {
		t.Errorf("clusterName = %q, want \"teleport.example.com\"", m.clusterName)
	}
}

func TestDialWithTokenError(t *testing.T) {
	m := &teleportModule{}
	if err := m.dialWithToken(""); err == nil {
		t.Errorf("dialWithToken(\"\") without broker: expected error, got nil")
	}
	if err := m.dial(); err == nil {
		t.Errorf("dial() without broker: expected error, got nil")
	}
}
