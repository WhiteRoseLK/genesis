// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/grpc"

	fleetagentv1 "genesis/sdk/go/gen/functions/fleet/agent/v1"
)

// fakeFleetAgentServer capture les appels Install reçus — joue le rôle d'un
// module « de parc » réel (teleport, J8), dispensé normalement via
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

// TestBuildSessionFanOutFleetFunction prouve docs/09-decisions.md ADR-017 :
// un seul appel Install(target) de l'appelant est diffusé vers TOUS les
// fournisseurs « de parc » installés, pas un seul fournisseur actif comme
// le reste des fonctions.
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
		Target: &fleetagentv1.Target{Host: "10.10.0.5", Port: 22, User: "genesis", SshPrivateKey: "clé-test"},
	}); err != nil {
		t.Fatalf("Install : %v", err)
	}

	if agentA.calls() != 1 || agentB.calls() != 1 {
		t.Fatalf("appels reçus = a:%d b:%d, attendu 1 partout (diffusion vers tous les fournisseurs)", agentA.calls(), agentB.calls())
	}
	if got := agentA.got[0].GetTarget().GetHost(); got != "10.10.0.5" {
		t.Errorf("target reçu par a = %q, attendu 10.10.0.5", got)
	}
}

// TestBuildSessionFleetFunctionWithNoProviders vérifie le cas no-op :
// aucun module « de parc » installé, l'appel échoue proprement (comme
// n'importe quelle fonction déclarée sans fournisseur — le module appelant
// doit traiter fleet.agent/v1 comme un requires optionnel, docs07/ADR-017).
func TestBuildSessionFleetFunctionWithNoProviders(t *testing.T) {
	r := NewRegistry()
	sessionServer := r.BuildSession("chrony", []string{"fleet.agent/v1"})
	sessionConn := serveInMemory(t, sessionServer)

	client := fleetagentv1.NewFleetAgentClient(sessionConn)
	_, err := client.Install(context.Background(), &fleetagentv1.InstallRequest{
		Target: &fleetagentv1.Target{Host: "10.10.0.5"},
	})
	if err == nil {
		t.Fatal("Install sans aucun fournisseur : succès inattendu")
	}
}

func serveFleetAgent(t *testing.T, agent fleetagentv1.FleetAgentServer) *grpc.ClientConn {
	t.Helper()
	s := grpc.NewServer()
	fleetagentv1.RegisterFleetAgentServer(s, agent)
	return serveInMemory(t, s)
}
