// SPDX-License-Identifier: Apache-2.0

package sdk

import "google.golang.org/protobuf/types/known/structpb"

// StateMap convertit StepRequest.state (éventuellement nil au premier appel)
// en map Go simple, pour qu'un module lise son propre état opaque
// (docs/03-module-contract.md §2 : "le module ne persiste rien lui-même").
func StateMap(s *structpb.Struct) map[string]any {
	if s == nil {
		return map[string]any{}
	}
	return s.AsMap()
}

// NewState encode une map Go en structpb.Struct pour StepResult.state.
func NewState(m map[string]any) (*structpb.Struct, error) {
	return structpb.NewStruct(m)
}
