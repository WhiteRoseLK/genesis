// SPDX-License-Identifier: Apache-2.0

// Package integration tests each module of the repository through the core's
// real module host (internal/modulehost, internal/broker), with simulated
// required functions (docs/03-module-contract.md, rule 7). The tests that
// drive real containers carry the `docker` build tag: `make test-docker`.
package integration
