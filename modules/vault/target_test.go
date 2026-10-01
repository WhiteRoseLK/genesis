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

func TestVaultTargetFromState(t *testing.T) {
	ctx := context.Background()
	pair := sshKeyPair{
		PrivateKeyOpenSSH:   "direct-private-key",
		PublicKeyAuthorized: "direct-public-key",
	}

	t.Run("direct ssh before teleport enrolment", func(t *testing.T) {
		m := &vaultModule{}
		st, err := sdk.NewState(map[string]any{
			"vm_ip":       "10.0.0.7",
			"vm_ssh_port": int64(22),
		})
		if err != nil {
			t.Fatalf("NewState: %v", err)
		}
		target, err := m.targetFromState(ctx, &modulev1.StepRequest{State: st}, pair)
		if err != nil {
			t.Fatalf("targetFromState: %v", err)
		}
		ansibleTarget := target.ansible()
		if ansibleTarget.GetHost() != "10.0.0.7" || ansibleTarget.GetPort() != 22 {
			t.Errorf("got host:port %s:%d, want 10.0.0.7:22", ansibleTarget.GetHost(), ansibleTarget.GetPort())
		}
		if ansibleTarget.GetSshPrivateKey() != "direct-private-key" {
			t.Errorf("got private key %q, want direct-private-key", ansibleTarget.GetSshPrivateKey())
		}
		if ansibleTarget.GetSshCertificatePem() != "" {
			t.Errorf("got certificate %q, want empty", ansibleTarget.GetSshCertificatePem())
		}
	})

	t.Run("teleport enrolled switches to port 3022 with certificate", func(t *testing.T) {
		fakeSSH := &fakeAccessSSHClient{}
		m := &vaultModule{accessSSHClient: fakeSSH}
		st, err := sdk.NewState(map[string]any{
			"vm_ip":        "10.0.0.7",
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
		ansibleTarget := target.ansible()
		if ansibleTarget.GetHost() != "10.0.0.7" || ansibleTarget.GetPort() != 3022 {
			t.Errorf("got host:port %s:%d, want 10.0.0.7:3022", ansibleTarget.GetHost(), ansibleTarget.GetPort())
		}
		if ansibleTarget.GetSshPrivateKey() != "KEY_PEM_DATA" {
			t.Errorf("got private key %q, want KEY_PEM_DATA", ansibleTarget.GetSshPrivateKey())
		}
		if ansibleTarget.GetSshCertificatePem() != "CERT_PEM_DATA" {
			t.Errorf("got certificate %q, want CERT_PEM_DATA", ansibleTarget.GetSshCertificatePem())
		}
	})
}

func TestVaultInstallFleetAgents(t *testing.T) {
	ctx := context.Background()

	t.Run("enrolment successful", func(t *testing.T) {
		m := &vaultModule{fleetAgentClient: &fakeFleetAgentClient{}}
		target := connTarget{Host: "10.0.0.7", Port: 22, User: "genesis", PrivateKey: "key"}
		enrolled, err := m.installFleetAgents(ctx, target)
		if err != nil {
			t.Fatalf("installFleetAgents: %v", err)
		}
		if !enrolled {
			t.Error("expected enrolled to be true")
		}
	})

	t.Run("unimplemented is silent no-op", func(t *testing.T) {
		m := &vaultModule{fleetAgentClient: &fakeFleetAgentClient{err: status.Error(codes.Unimplemented, "unimplemented")}}
		target := connTarget{Host: "10.0.0.7", Port: 22, User: "genesis", PrivateKey: "key"}
		enrolled, err := m.installFleetAgents(ctx, target)
		if err != nil {
			t.Fatalf("installFleetAgents: %v", err)
		}
		if enrolled {
			t.Error("expected enrolled to be false")
		}
	})
}
