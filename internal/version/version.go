// SPDX-License-Identifier: Apache-2.0

// Package version holds the core's version, compared with the `core`
// constraint of each module's manifest (docs/03-module-contract.md).
package version

// Version is updated by Release Please in the release PR (marker below): the
// published binary, the tag and the module compatibility check therefore
// always refer to the same version.
const Version = "0.2.0" // x-release-please-version
