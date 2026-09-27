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

	echov1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/test/echo/v1"
)

// serveInMemory starts s on an in-memory listener (bufconn) and returns a
// client connection to it, as a real Dial would — useful to test the broker's
// routing without a real go-plugin process.
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
		t.Fatalf("in-memory connection: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// echoProvider is a minimal test.*/v1 provider, playing the role of a real
// provider module (normally dispensed through go-plugin).
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

// TestBuildSessionRoutesDeclaredFunction checks that a declared (allowed)
// function is routed to its active provider.
func TestBuildSessionRoutesDeclaredFunction(t *testing.T) {
	providerConn := newEchoProviderConn(t, "test-a")

	r := NewRegistry()
	r.SetModuleProvider("test.a/v1", providerConn, ForwardEcho)

	sessionServer := r.BuildSession("test-b", []string{"test.a/v1"})
	sessionConn := serveInMemory(t, sessionServer)

	client := echov1.NewEchoClient(sessionConn)
	resp, err := client.Call(context.Background(), &echov1.CallRequest{Message: "hello"})
	if err != nil {
		t.Fatalf("Call on a declared function: %v", err)
	}
	if resp.GetFrom() != "test-a" || resp.GetMessage() != "echo: hello" {
		t.Errorf("response = %+v, want from=test-a message=\"echo: hello\"", resp)
	}
}

// TestBuildSessionRefusesUndeclaredFunction is the M4 acceptance criterion
// (doc 08): "a call to an undeclared function is refused by the broker". The
// function does have an active provider (test-a exists), but it is not in the
// caller's `allowed`: it is never registered on its session, so the call fails
// with Unimplemented.
func TestBuildSessionRefusesUndeclaredFunction(t *testing.T) {
	providerConn := newEchoProviderConn(t, "test-a")

	r := NewRegistry()
	r.SetModuleProvider("test.a/v1", providerConn, ForwardEcho)

	// test-c does NOT declare test.a/v1 in its requires.
	sessionServer := r.BuildSession("test-c", []string{})
	sessionConn := serveInMemory(t, sessionServer)

	client := echov1.NewEchoClient(sessionConn)
	_, err := client.Call(context.Background(), &echov1.CallRequest{Message: "hello"})
	if err == nil {
		t.Fatal("Call on an undeclared function: unexpected success, should have been refused")
	}
	if status.Code(err) != codes.Unimplemented {
		t.Errorf("code = %v, want %v (Unimplemented)", status.Code(err), codes.Unimplemented)
	}
}

func TestHasProvider(t *testing.T) {
	r := NewRegistry()
	if r.HasProvider("test.a/v1") {
		t.Error("HasProvider before any registration: unexpected true")
	}
	r.SetModuleProvider("test.a/v1", newEchoProviderConn(t, "test-a"), ForwardEcho)
	if !r.HasProvider("test.a/v1") {
		t.Error("HasProvider after SetModuleProvider: unexpected false")
	}
	r.Unset("test.a/v1")
	if r.HasProvider("test.a/v1") {
		t.Error("HasProvider after Unset: unexpected true")
	}
}
