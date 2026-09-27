// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/grpc"

	fleetagentv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
)

// fakeFleetAgentServer records the Install calls it receives — it plays the
// role of a real "fleet" module (teleport, M8), normally dispensed through
// go-plugin.
type fakeFleetAgentServer struct {
	fleetagentv1.UnimplementedFleetAgentServer
	name string
	mu   sync.Mutex
	got  []*fleetagentv1.InstallRequest
}

func (f *fakeFleetAgentServer) Install(_ context.Context, req *fleetagentv1.InstallRequest) (*fleetagentv1.InstallResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, req)
	return &fleetagentv1.InstallResponse{}, nil
}

func (f *fakeFleetAgentServer) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

// TestBuildSessionFanOutFleetFunction proves docs/09-decisions.md ADR-017: a
// single Install(target) call from the caller fans out to ALL installed
// "fleet" providers, not to a single active provider as for the other
// functions.
func TestBuildSessionFanOutFleetFunction(t *testing.T) {
	agentA := &fakeFleetAgentServer{name: "teleport-like-a"}
	agentB := &fakeFleetAgentServer{name: "teleport-like-b"}

	connA := serveFleetAgent(t, agentA)
	connB := serveFleetAgent(t, agentB)

	r := NewRegistry()
	r.AddFleetProvider("fleet.agent/v1", connA)
	r.AddFleetProvider("fleet.agent/v1", connB)

	sessionServer := r.BuildSession("chrony", []string{"fleet.agent/v1"})
	sessionConn := serveInMemory(t, sessionServer)

	client := fleetagentv1.NewFleetAgentClient(sessionConn)
	if _, err := client.Install(context.Background(), &fleetagentv1.InstallRequest{
		Target: &fleetagentv1.Target{Host: "10.10.0.5", Port: 22, User: "genesis", SshPrivateKey: "test-key"},
	}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	if agentA.calls() != 1 || agentB.calls() != 1 {
		t.Fatalf("calls received = a:%d b:%d, want 1 each (fan-out to every provider)", agentA.calls(), agentB.calls())
	}
	if got := agentA.got[0].GetTarget().GetHost(); got != "10.10.0.5" {
		t.Errorf("target received by a = %q, want 10.10.0.5", got)
	}
}

// TestBuildSessionFleetFunctionWithNoProviders checks the no-op case: no
// "fleet" module is installed, the call fails cleanly (like any declared
// function without a provider — the calling module must treat fleet.agent/v1
// as an optional requires, doc 07/ADR-017).
func TestBuildSessionFleetFunctionWithNoProviders(t *testing.T) {
	r := NewRegistry()
	sessionServer := r.BuildSession("chrony", []string{"fleet.agent/v1"})
	sessionConn := serveInMemory(t, sessionServer)

	client := fleetagentv1.NewFleetAgentClient(sessionConn)
	_, err := client.Install(context.Background(), &fleetagentv1.InstallRequest{
		Target: &fleetagentv1.Target{Host: "10.10.0.5"},
	})
	if err == nil {
		t.Fatal("Install with no provider: unexpected success")
	}
}

func serveFleetAgent(t *testing.T, agent fleetagentv1.FleetAgentServer) *grpc.ClientConn {
	t.Helper()
	s := grpc.NewServer()
	fleetagentv1.RegisterFleetAgentServer(s, agent)
	return serveInMemory(t, s)
}
