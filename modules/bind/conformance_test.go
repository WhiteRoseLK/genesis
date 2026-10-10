// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	sdk "github.com/WhiteRoseLK/genesis/sdk/go"
	"github.com/WhiteRoseLK/genesis/sdk/go/moduletest"
)

// TestConformance: the SDK conformance suite (docs/10-adding-a-module.md,
// step 5). Run it with 'go test ./... -run Conformance'.
func TestConformance(t *testing.T) {
	mf, err := sdk.LoadManifest("module.yaml")
	if err != nil {
		t.Fatalf("loading module.yaml: %v", err)
	}
	impl := &bindModule{manifest: mf.ToProto()}
	moduletest.RunConformance(t, impl, "module.yaml")
}
