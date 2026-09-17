// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	echov1 "genesis/sdk/go/gen/functions/test/echo/v1"
)

// serveInMemory démarre s sur un listener en mémoire (bufconn) et retourne
// une connexion cliente dessus, comme le ferait un Dial réel — utile pour
// tester le routage du broker sans process go-plugin réel.
func serveInMemory(t *testing.T, s *grpc.Server) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	go func() { _ = s.Serve(lis) }()
	t.Cleanup(s.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("connexion en mémoire : %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// echoProvider est un fournisseur test.*/v1 minimal, jouant le rôle d'un
// module fournisseur réel (dispensé normalement via go-plugin).
type echoProvider struct {
	echov1.UnimplementedEchoServer
	name string
}

func (e *echoProvider) Call(_ context.Context, req *echov1.CallRequest) (*echov1.CallResponse, error) {
	return &echov1.CallResponse{Message: "echo: " + req.GetMessage(), From: e.name, Phase: "target"}, nil
}

func newEchoProviderConn(t *testing.T, name string) *grpc.ClientConn {
	t.Helper()
	s := grpc.NewServer()
	echov1.RegisterEchoServer(s, &echoProvider{name: name})
	return serveInMemory(t, s)
}

// TestBuildSessionRoutesDeclaredFunction vérifie qu'une fonction déclarée
// (allowed) est bien routée vers son fournisseur actif.
func TestBuildSessionRoutesDeclaredFunction(t *testing.T) {
	providerConn := newEchoProviderConn(t, "test-a")

	r := NewRegistry()
	r.SetModuleProvider("test.a/v1", providerConn, ForwardEcho)

	sessionServer := r.BuildSession("test-b", []string{"test.a/v1"})
	sessionConn := serveInMemory(t, sessionServer)

	client := echov1.NewEchoClient(sessionConn)
	resp, err := client.Call(context.Background(), &echov1.CallRequest{Message: "salut"})
	if err != nil {
		t.Fatalf("Call sur une fonction déclarée : %v", err)
	}
	if resp.GetFrom() != "test-a" || resp.GetMessage() != "echo: salut" {
		t.Errorf("réponse = %+v, attendu from=test-a message=\"echo: salut\"", resp)
	}
}

// TestBuildSessionRefusesUndeclaredFunction est le critère d'acceptation du
// jalon J4 (doc 08) : "appel d'une fonction non déclarée refusé par le
// broker". La fonction a bien un fournisseur actif (test-a existe), mais
// n'est pas dans le `allowed` de l'appelant : elle n'est jamais enregistrée
// sur sa session, l'appel échoue donc avec Unimplemented.
func TestBuildSessionRefusesUndeclaredFunction(t *testing.T) {
	providerConn := newEchoProviderConn(t, "test-a")

	r := NewRegistry()
	r.SetModuleProvider("test.a/v1", providerConn, ForwardEcho)

	// test-c ne déclare PAS test.a/v1 dans son requires.
	sessionServer := r.BuildSession("test-c", []string{})
	sessionConn := serveInMemory(t, sessionServer)

	client := echov1.NewEchoClient(sessionConn)
	_, err := client.Call(context.Background(), &echov1.CallRequest{Message: "salut"})
	if err == nil {
		t.Fatal("Call sur une fonction non déclarée : succès inattendu, devait être refusé")
	}
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("code = %v, attendu %v (Unimplemented)", status.Code(err), codes.Unimplemented)
	}
}

func TestHasProvider(t *testing.T) {
	r := NewRegistry()
	if r.HasProvider("test.a/v1") {
		t.Error("HasProvider avant tout enregistrement : true inattendu")
	}
	r.SetModuleProvider("test.a/v1", newEchoProviderConn(t, "test-a"), ForwardEcho)
	if !r.HasProvider("test.a/v1") {
		t.Error("HasProvider après SetModuleProvider : false inattendu")
	}
	r.Unset("test.a/v1")
	if r.HasProvider("test.a/v1") {
		t.Error("HasProvider après Unset : true inattendu")
	}
}
