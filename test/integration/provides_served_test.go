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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	_ "google.golang.org/protobuf/types/known/emptypb"

	"github.com/WhiteRoseLK/genesis/internal/modulehost"
	sdk "github.com/WhiteRoseLK/genesis/sdk/go"

	// The generated code registers the function descriptors in protoregistry.
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/access/ssh/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/compute/vm/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/ansible/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/container/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/core/secrets/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/resolver/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/dns/zone/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/fleet/agent/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/os/base/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/pki/issuer/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/secrets/kv/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/test/echo/v1"
	_ "github.com/WhiteRoseLK/genesis/sdk/go/gen/functions/time/ntp/v1"
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

// functionServices returns, from the proto file descriptors in files, the
// full names of the services that define a function, mapped to their number
// of methods. A descriptor carries no function name, so the anchor is the
// directory of the .proto file in sdk/proto: "dns.zone/v1" is defined by the
// files under "functions/dns/zone/v1/", whatever their proto package is.
func functionServices(files *protoregistry.Files, function string) map[protoreflect.FullName]int {
	name, version, _ := strings.Cut(function, "/")
	dir := "functions/" + strings.ReplaceAll(name, ".", "/") + "/" + version + "/"
	services := map[protoreflect.FullName]int{}
	files.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		if !strings.HasPrefix(fd.Path(), dir) || strings.Contains(fd.Path()[len(dir):], "/") {
			return true
		}
		for i := 0; i < fd.Services().Len(); i++ {
			sd := fd.Services().Get(i)
			services[sd.FullName()] = sd.Methods().Len()
		}
		return true
	})
	return services
}

// functionServed reports whether one of the served services is a service of
// the function, according to the descriptors in files. A service without any
// method does not count: it could not provide the function.
func functionServed(files *protoregistry.Files, function string, served []string) bool {
	services := functionServices(files, function)
	for _, s := range served {
		if methods, ok := services[protoreflect.FullName(s)]; ok && methods > 0 {
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
		if !functionServed(protoregistry.GlobalFiles, fn.Function, served) {
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

// TestFunctionServicesFromDescriptors checks the function -> service table on
// the descriptors of every function of the SDK, and on a service whose proto
// package does not follow the "functions.<name>.<version>" layout.
func TestFunctionServicesFromDescriptors(t *testing.T) {
	for _, fn := range []string{
		"access.ssh/v1", "compute.vm/v1", "core.ansible/v1", "core.container/v1",
		"core.secrets/v1", "dns.resolver/v1", "dns.zone/v1", "fleet.agent/v1",
		"os.base/v1", "pki.issuer/v1", "secrets.kv/v1", "time.ntp/v1", "test.echo/v1",
	} {
		services := functionServices(protoregistry.GlobalFiles, fn)
		if len(services) != 1 {
			t.Errorf("%s: want exactly one service, got %v", fn, services)
		}
		for name, methods := range services {
			if methods == 0 {
				t.Errorf("%s: service %s has no method", fn, name)
			}
		}
	}

	files := new(protoregistry.Files)
	register := func(path, pkg, service string, methods int) {
		t.Helper()
		svc := &descriptorpb.ServiceDescriptorProto{Name: proto.String(service)}
		for i := 0; i < methods; i++ {
			svc.Method = append(svc.Method, &descriptorpb.MethodDescriptorProto{
				Name:       proto.String(fmt.Sprintf("Call%d", i)),
				InputType:  proto.String(".google.protobuf.Empty"),
				OutputType: proto.String(".google.protobuf.Empty"),
			})
		}
		fd, err := protodesc.NewFile(&descriptorpb.FileDescriptorProto{
			Name:       proto.String(path),
			Package:    proto.String(pkg),
			Dependency: []string{"google/protobuf/empty.proto"},
			Service:    []*descriptorpb.ServiceDescriptorProto{svc},
			Syntax:     proto.String("proto3"),
		}, protoregistry.GlobalFiles)
		if err != nil {
			t.Fatal(err)
		}
		if err := files.RegisterFile(fd); err != nil {
			t.Fatal(err)
		}
	}
	register("functions/odd/thing/v1/thing.proto", "legacy.oddities", "Odd", 1)
	register("functions/empty/thing/v1/thing.proto", "functions.empty.thing.v1", "Empty", 0)

	if !functionServed(files, "odd.thing/v1", []string{"legacy.oddities.Odd"}) {
		t.Error("odd.thing/v1 served from a differently named package should be matched")
	}
	if functionServed(files, "odd.thing/v1", []string{"functions.odd.thing.v1.Odd"}) {
		t.Error("a service name that is not in the descriptors must not match by prefix")
	}
	if functionServed(files, "empty.thing/v1", []string{"functions.empty.thing.v1.Empty"}) {
		t.Error("a service without any method must not count as serving the function")
	}
	if functionServed(files, "missing.thing/v1", []string{"functions.missing.thing.v1.X"}) {
		t.Error("a function without descriptors must not be served")
	}
}
