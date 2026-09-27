// SPDX-License-Identifier: Apache-2.0

// Package sdk is the only dependency allowed for a module
// (docs/02-architecture.md, docs/03-module-contract.md): the generated
// module/v1 protocol (sdk/go/gen), Serve to expose it as a go-plugin plugin,
// and loading of the module.yaml manifest.
package sdk
