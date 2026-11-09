// SPDX-License-Identifier: Apache-2.0

// Package moduletest is the SDK test harness: the conformance suite a module
// must pass before it is considered done (docs/10-adding-a-module.md,
// docs/03-module-contract.md §4).
//
// Checks here never need the broker or a built module binary (Describe
// consistent with the manifest, Validate does not crash, Check is
// repeatable). Checks that need a running module — every declared function
// is really served — live in test/integration instead (issue #145).
package moduletest

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"
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

	// Check must be repeatable: the engine's replay logic ("Check before any
	// action", AGENTS.md) relies on it not flip-flopping for the same input.
	stepReq := &modulev1.StepRequest{Config: emptyConfig}
	first, firstErr := impl.Check(ctx, stepReq)
	second, secondErr := impl.Check(ctx, stepReq)
	switch {
	case (firstErr == nil) != (secondErr == nil):
		t.Errorf("Check is not repeatable: first call error = %v, second call error = %v", firstErr, secondErr)
	case firstErr == nil && !proto.Equal(first, second):
		t.Errorf("Check is not repeatable: got %v then %v for the same input", first, second)
	}
}
