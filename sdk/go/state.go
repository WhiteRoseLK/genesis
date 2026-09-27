// SPDX-License-Identifier: Apache-2.0

package sdk

import "google.golang.org/protobuf/types/known/structpb"

// StateMap converts StepRequest.state (possibly nil on the first call) into a
// plain Go map, so that a module can read its own opaque state
// (docs/03-module-contract.md §2: "the module persists nothing itself").
func StateMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return map[string]any{}
	}
	return s.AsMap()
}

// NewState encodes a Go map into a structpb.Struct for StepResult.state.
func NewState(m map[string]any) (*structpb.Struct, error) {
	return structpb.NewStruct(m)
}
