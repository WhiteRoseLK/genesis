// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	accesssshv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
	ansiblev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

type fakeAccessSSHClient struct {
	called     bool
	principals []string
}

func (f *fakeAccessSSHClient) JumpHost(context.Context, *accesssshv1.Empty, ...grpc.CallOption) (*accesssshv1.JumpHostInfo, error) {
	return nil, status.Error(codes.Unimplemented, "unimplemented")
}

func (f *fakeAccessSSHClient) SignUserKey(_ context.Context, in *accesssshv1.SignUserKeyRequest, _ ...grpc.CallOption) (*accesssshv1.SignUserKeyResponse, error) {
	f.called = true
	f.principals = in.GetPrincipals()
	return &accesssshv1.SignUserKeyResponse{
		CertificateOpenssh: "CERT_PEM_DATA",
		PrivateKeyOpenssh:  "KEY_PEM_DATA",
	}, nil
}

type fakeFleetAgentClient struct {
	err error
}

func (f *fakeFleetAgentClient) Install(context.Context, *fleetagentv1.InstallRequest, ...grpc.CallOption) (*fleetagentv1.InstallResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &fleetagentv1.InstallResponse{}, nil
}

func TestChronyTargetFromState(t *testing.T) {
	ctx := context.Background()
	pair := sshKeyPair{
		PrivateKeyOpenSSH:   "direct-private-key",
		PublicKeyAuthorized: "direct-public-key",
	}

	t.Run("direct ssh before teleport enrolment", func(t *testing.T) {
		m := &chronyModule{}
		st, err := sdk.NewState(map[string]any{
			"vm_ip":       "10.0.0.5",
			"vm_ssh_port": int64(22),
		})
		if err != nil {
			t.Fatalf("NewState: %v", err)
		}
		target, err := m.targetFromState(ctx, &modulev1.StepRequest{State: st}, pair)
		if err != nil {
			t.Fatalf("targetFromState: %v", err)
		}
		if target.GetHost() != "10.0.0.5" || target.GetPort() != 22 {
			t.Errorf("got host:port %s:%d, want 10.0.0.5:22", target.GetHost(), target.GetPort())
		}
		if target.GetSshPrivateKey() != "direct-private-key" {
			t.Errorf("got private key %q, want %q", target.GetSshPrivateKey(), "direct-private-key")
		}
		if target.GetSshCertificatePem() != "" {
			t.Errorf("got certificate %q, want empty", target.GetSshCertificatePem())
		}
	})

	t.Run("teleport enrolled switches to port 3022 with certificate", func(t *testing.T) {
		fakeSSH := &fakeAccessSSHClient{}
		m := &chronyModule{accessSSHClient: fakeSSH}
		st, err := sdk.NewState(map[string]any{
			"vm_ip":        "10.0.0.5",
			"vm_ssh_port":  int64(22),
			"admin_method": "teleport",
		})
		if err != nil {
			t.Fatalf("NewState: %v", err)
		}
		target, err := m.targetFromState(ctx, &modulev1.StepRequest{State: st}, pair)
		if err != nil {
			t.Fatalf("targetFromState: %v", err)
		}
		if !fakeSSH.called {
			t.Error("expected access.ssh/v1.SignUserKey to be called")
		}
		if len(fakeSSH.principals) != 1 || fakeSSH.principals[0] != sshUser {
			t.Errorf("SignUserKey principals = %v, want [%s]", fakeSSH.principals, sshUser)
		}
		if target.GetHost() != "10.0.0.5" || target.GetPort() != 3022 {
			t.Errorf("got host:port %s:%d, want 10.0.0.5:3022", target.GetHost(), target.GetPort())
		}
		if target.GetSshPrivateKey() != "KEY_PEM_DATA" {
			t.Errorf("got private key %q, want KEY_PEM_DATA", target.GetSshPrivateKey())
		}
		if target.GetSshCertificatePem() != "CERT_PEM_DATA" {
			t.Errorf("got certificate %q, want CERT_PEM_DATA", target.GetSshCertificatePem())
		}
	})
}

func TestChronyInstallFleetAgents(t *testing.T) {
	ctx := context.Background()

	t.Run("enrolment successful", func(t *testing.T) {
		m := &chronyModule{fleetAgentClient: &fakeFleetAgentClient{}}
		target := &ansiblev1.Target{Host: "10.0.0.5", Port: 22, User: "genesis", SshPrivateKey: "key"}
		enrolled, err := m.installFleetAgents(ctx, target)
		if err != nil {
			t.Fatalf("installFleetAgents: %v", err)
		}
		if !enrolled {
			t.Error("expected enrolled to be true")
		}
	})

	t.Run("unimplemented is silent no-op", func(t *testing.T) {
		m := &chronyModule{fleetAgentClient: &fakeFleetAgentClient{err: status.Error(codes.Unimplemented, "unimplemented")}}
		target := &ansiblev1.Target{Host: "10.0.0.5", Port: 22, User: "genesis", SshPrivateKey: "key"}
		enrolled, err := m.installFleetAgents(ctx, target)
		if err != nil {
			t.Fatalf("installFleetAgents: %v", err)
		}
		if enrolled {
			t.Error("expected enrolled to be false")
		}
	})
}
