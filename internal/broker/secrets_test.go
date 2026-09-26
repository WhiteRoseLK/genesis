// SPDX-License-Identifier: Apache-2.0

package broker

import (
	"context"
	"testing"

	"filippo.io/age"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/WhiteRoseLK/genesis/internal/secrets"
	secretsv1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
)

func newTestSecretsRegistry(t *testing.T) *Registry {
	t.Helper()
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	store := secrets.NewFileStore(t.TempDir(), identity)

	r := NewRegistry()
	r.SetNative("core.secrets/v1", NativeSecrets(store))
	return r
}

func secretsClientFor(t *testing.T, r *Registry, caller string) secretsv1.SecretsClient {
	t.Helper()
	server := r.BuildSession(caller, []string{"core.secrets/v1"})
	conn := serveInMemory(t, server)
	return secretsv1.NewSecretsClient(conn)
}

func TestSecretsEnsureAndGetSameCaller(t *testing.T) {
	r := newTestSecretsRegistry(t)
	client := secretsClientFor(t, r, "module-a")
	ctx := context.Background()

	_, err := client.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       "module-a/token",
		Generator: secretsv1.Generator_GENERATOR_TOKEN,
	})
	if err != nil {
		t.Fatalf("Ensure : %v", err)
	}

	resp, err := client.Get(ctx, &secretsv1.GetRequest{Ref: "module-a/token"})
	if err != nil {
		t.Fatalf("Get par le propriétaire : %v", err)
	}
	if resp.GetValue() == "" {
		t.Error("Get a retourné une valeur vide")
	}
}

// TestSecretsGetDeniedForOtherModule est le pendant applicatif de la règle
// "un module ne peut lire que ses propres secrets et ceux explicitement
// partagés" (docs/02-architecture.md).
func TestSecretsGetDeniedForOtherModule(t *testing.T) {
	r := newTestSecretsRegistry(t)
	ctx := context.Background()

	owner := secretsClientFor(t, r, "module-a")
	if _, err := owner.Ensure(ctx, &secretsv1.EnsureRequest{Ref: "module-a/secret", Generator: secretsv1.Generator_GENERATOR_TOKEN}); err != nil {
		t.Fatalf("Ensure : %v", err)
	}

	other := secretsClientFor(t, r, "module-b")
	_, err := other.Get(ctx, &secretsv1.GetRequest{Ref: "module-a/secret"})
	if err == nil {
		t.Fatal("Get par un autre module : succès inattendu, devait être refusé")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("code = %v, attendu %v", status.Code(err), codes.PermissionDenied)
	}
}

// TestSecretsGetAllowedForExplicitConsumer vérifie le partage explicite.
func TestSecretsGetAllowedForExplicitConsumer(t *testing.T) {
	r := newTestSecretsRegistry(t)
	ctx := context.Background()

	owner := secretsClientFor(t, r, "module-a")
	_, err := owner.Ensure(ctx, &secretsv1.EnsureRequest{
		Ref:       "module-a/shared",
		Generator: secretsv1.Generator_GENERATOR_TOKEN,
		Meta:      &secretsv1.Meta{Consumers: []string{"module-b"}},
	})
	if err != nil {
		t.Fatalf("Ensure : %v", err)
	}

	consumer := secretsClientFor(t, r, "module-b")
	if _, err := consumer.Get(ctx, &secretsv1.GetRequest{Ref: "module-a/shared"}); err != nil {
		t.Errorf("Get par un consumer déclaré : erreur inattendue : %v", err)
	}
}

func TestSecretsEnsureDeniedForWrongOwner(t *testing.T) {
	r := newTestSecretsRegistry(t)
	client := secretsClientFor(t, r, "module-a")

	_, err := client.Ensure(context.Background(), &secretsv1.EnsureRequest{
		Ref:       "impersonation",
		Generator: secretsv1.Generator_GENERATOR_TOKEN,
		Meta:      &secretsv1.Meta{Owner: "module-b"},
	})
	if err == nil {
		t.Fatal("Ensure avec owner falsifié : succès inattendu")
	}
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("code = %v, attendu %v", status.Code(err), codes.PermissionDenied)
	}
}
