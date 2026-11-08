// SPDX-License-Identifier: Apache-2.0

package integration

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
)

// servedServices lists, through gRPC reflection, the services a launched
// module really serves on the connection of one of its functions. No function
// is called: reflection only describes the server.
func servedServices(ctx context.Context, conn *grpc.ClientConn) ([]string, error) {
	stream, err := reflectionpb.NewServerReflectionClient(conn).ServerReflectionInfo(ctx)
	if err != nil {
		return nil, err
	}
	if err := stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_ListServices{ListServices: ""},
	}); err != nil {
		return nil, err
	}
	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	var names []string
	for _, s := range resp.GetListServicesResponse().GetService() {
		names = append(names, s.GetName())
	}
	sort.Strings(names)
	return names, stream.CloseSend()
}

// functionServed reports whether one of the served services belongs to the
// proto package of the function: "dns.zone/v1" -> "functions.dns.zone.v1.*"
// (the layout of sdk/proto).
func functionServed(function string, served []string) bool {
	prefix := "functions." + strings.ReplaceAll(function, "/", ".") + "."
	for _, s := range served {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// unservedProvides launches the module and returns one message per function of
// manifest.Provides that the running module does not serve.
func unservedProvides(binaryPath string, manifest *sdk.ManifestFile) ([]string, error) {
	client, err := modulehost.Launch(binaryPath, manifest)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var problems []string
	for _, fn := range manifest.Provides {
		conn, err := client.DispenseFunction(fn.Function)
		if err != nil {
			return nil, err
		}
		served, err := servedServices(ctx, conn)
		if err != nil {
			return nil, fmt.Errorf("listing served services (is gRPC reflection registered?): %w", err)
		}
		if !functionServed(fn.Function, served) {
			problems = append(problems, fmt.Sprintf("%s declared in provides but not served (served: %v)", fn.Function, served))
		}
	}
	return problems, nil
}

// TestModulesServeTheirProvides checks, from the outside, that every function
// declared in `provides` by a module's module.yaml is really served by the
// built module (issue #145, first check).
func TestModulesServeTheirProvides(t *testing.T) {
	manifests, err := filepath.Glob(filepath.Join("..", "..", "modules", "*", "module.yaml"))
	if err != nil || len(manifests) == 0 {
		t.Fatalf("no module found: %v", err)
	}
	for _, manifestPath := range manifests {
		dir := filepath.Dir(manifestPath)
		name := filepath.Base(dir)
		t.Run(name, func(t *testing.T) {
			manifest, err := sdk.LoadManifest(manifestPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(manifest.Provides) == 0 {
				t.Skip("provides no function")
			}
			problems, err := unservedProvides(buildModuleAt(t, dir), manifest)
			if err != nil {
				t.Fatalf("module %s: %v", name, err)
			}
			for _, p := range problems {
				t.Errorf("module %s: %s", name, p)
			}
		})
	}
}

// buildModuleAt compiles the module whose sources are in sourceDir.
func buildModuleAt(t *testing.T, sourceDir string) string {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), modulehost.BinaryName())
	build := exec.Command("go", "build", "-o", binaryPath, ".")
	build.Dir = sourceDir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("compiling %s: %v\n%s", sourceDir, err, out)
	}
	return binaryPath
}

// TestProvidesCheckDetectsUnservedFunction proves the check can fail: test-kv
// is run with a manifest that also declares a function it does not register.
func TestProvidesCheckDetectsUnservedFunction(t *testing.T) {
	dir := filepath.Join("..", "modules", "test-kv")
	manifest, err := sdk.LoadManifest(filepath.Join(dir, "module.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	manifest.Provides = append(manifest.Provides, sdk.FunctionRef{Function: "bogus.thing/v1"})

	problems, err := unservedProvides(buildModuleAt(t, dir), manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 1 || !strings.HasPrefix(problems[0], "bogus.thing/v1 ") {
		t.Fatalf("want exactly bogus.thing/v1 reported, got %q", problems)
	}
}
