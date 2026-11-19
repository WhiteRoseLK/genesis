// SPDX-License-Identifier: Apache-2.0

// Package moduletest is the SDK test harness: the conformance suite a module
// must pass before it is considered done (docs/10-adding-a-module.md,
// docs/03-module-contract.md §4).
//
// Skeleton at milestone M3: only the checks that do not depend on the broker
// are done here (Describe consistent with the manifest, Validate does not
// crash). The full semantic checks — replayed idempotence, no secret in the
// outputs, compliance of each provided function with simulated required
// functions — need the broker and arrive at milestone M4
// (docs/08-milestones.md).
package moduletest

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	modulev1 "github.com/WhiteRoseLK/genesis/sdk/go/gen/module/v1"
)

// RunConformance runs the minimal conformance suite on impl, whose manifest is
// at manifestPath (docs/10-adding-a-module.md, step 5: `go test ./... -run
// Conformance`).
func RunConformance(t *testing.T, impl modulev1.ModuleServer, manifestPath string) {
	t.Helper()
	ctx := context.Background()

	manifest, err := sdk.LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("loading %s: %v", manifestPath, err)
	}
	want := manifest.ToProto()

	got, err := impl.Describe(ctx, &modulev1.Empty{})
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	if got.GetName() != want.GetName() {
		t.Errorf("Describe().Name = %q, want %q (module.yaml)", got.GetName(), want.GetName())
	}
	if got.GetVersion() != want.GetVersion() {
		t.Errorf("Describe().Version = %q, want %q (module.yaml)", got.GetVersion(), want.GetVersion())
	}
	if len(got.GetProvides()) != len(want.GetProvides()) {
		t.Errorf("Describe().Provides has %d entry(ies), want %d (module.yaml)", len(got.GetProvides()), len(want.GetProvides()))
	}

	emptyConfig, err := structpb.NewStruct(map[string]any{})
	if err != nil {
		t.Fatalf("building an empty config: %v", err)
	}
	if _, err := impl.Validate(ctx, &modulev1.ValidateRequest{Config: emptyConfig}); err != nil {
		t.Errorf("Validate with an empty config returned a transport error rather than Diagnostics: %v", err)
	}

	checkNoSecretInOutputs(t, impl)
}

// secretConfigKeys are the config keys a module is most likely to treat as
// secrets; each is given its own sentinel value so a leak names its source.
var secretConfigKeys = []string{"password", "token", "secret", "api_key", "private_key"}

func sentinel(key string) string { return "genesis-conformance-secret-" + key }

// checkNoSecretInOutputs gives Validate and Check a config whose secret-looking
// values are unmistakable sentinels, and fails if any of them comes back in a
// diagnostic, a check detail or an error ("no plaintext secret in logs,
// outputs, errors", AGENTS.md). Only these two RPCs are called: they are the
// ones that must be safe to run with no side effect.
func checkNoSecretInOutputs(t *testing.T, impl modulev1.ModuleServer) {
	t.Helper()
	ctx := context.Background()

	values := make(map[string]any, len(secretConfigKeys))
	for _, key := range secretConfigKeys {
		values[key] = sentinel(key)
	}
	config, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatalf("building a config with sentinel secrets: %v", err)
	}

	assertNoSentinel := func(rpc, output string) {
		for _, key := range secretConfigKeys {
			if strings.Contains(output, sentinel(key)) {
				t.Errorf("%s output contains the value of the %q config key: %s", rpc, key, output)
			}
		}
	}

	diags, err := impl.Validate(ctx, &modulev1.ValidateRequest{Config: config})
	if err != nil {
		assertNoSentinel("Validate error", err.Error())
	}
	for _, d := range diags.GetDiagnostics() {
		assertNoSentinel("Validate diagnostic", d.GetPath()+" "+d.GetMessage())
	}

	result, err := impl.Check(ctx, &modulev1.StepRequest{Config: config})
	if err != nil {
		assertNoSentinel("Check error", err.Error())
	}
	assertNoSentinel("Check detail", result.GetDetail())
}
