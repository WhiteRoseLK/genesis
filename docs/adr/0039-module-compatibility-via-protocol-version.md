# ADR-0039 — Module compatibility via protocol version rather than core release version

- **Status**: accepted
- **Discussion**: [#39](https://github.com/WhiteRoseLK/genesis/issues/39)

Context:
Originally (M3, ADR-021), module compatibility was validated exclusively against the `core:` semver constraint declared in each module's `module.yaml` manifest, compared against the published version of the core binary in `internal/version`.
Prior to 1.0, any breaking change bumps the minor version of the core (e.g., 0.1.0 → 0.2.0 → 0.3.0). As encountered during the 0.2.0 release (PR #38 / #40), breaking changes to core CLI internals or SDK Go package import paths bumped the core minor version without altering the core ↔ module gRPC runtime interface. Because in-repo modules declared upper bounds such as `<0.2.0`, the release PR failed in CI as every module was rejected by the resolver.
The temporary workaround required manually bumping the upper bound on all modules (e.g. `<0.3.0`) in every PR introducing a breaking change. This created ongoing maintenance overhead and meant third-party compiled modules would be rejected by newer core releases even when completely compatible.

Alternatives:
(a) Retain the upper bound constraint (`<0.X.0`) on all modules and require every breaking change PR to bump the constraint across all module manifests in the monorepo — rejected: fragile, high maintenance burden, and fundamentally incompatible with distributing independent third-party modules.
(b) Eliminate compatibility checks altogether — rejected: protocol changes would fail during go-plugin RPC communication or handshake negotiation rather than failing cleanly during module resolution and plan generation.
(c) Base runtime compatibility primarily on the lifecycle protocol version (`protocol: 1` in `module.yaml`), with `core:` retained as an optional semver constraint for exceptional needs — accepted: cleanly separates runtime wire compatibility from core application versioning.

Decision:
- Runtime compatibility between the core and modules is primarily evaluated via the lifecycle protocol version (`protocol: 1` in `module.yaml`), which corresponds to `sdk.ProtocolVersion` and the go-plugin handshake protocol version.
- `resolver.Resolve` verifies that each resolved module declares a protocol version matching the core's supported protocol version (`1`). Any module declaring an unsupported or missing protocol is rejected with an actionable error.
- The `core:` manifest field is optional. When specified, it is evaluated as a semver constraint against the core's version, allowing modules to declare a minimum required core engine feature without imposing an artificial upper bound.
- Modules in this repository drop the artificial upper bound (`<0.3.0`), declaring `core: ">=0.1.0"`.
- The module `scaffold` template generates `protocol: 1` and `core: ">=0.1.0"`.
- Contributing documentation is updated so PRs with breaking changes no longer need to bump module upper bounds.

Consequences:
- Minor core releases no longer invalidate installed modules unless the lifecycle wire protocol version itself changes.
- Third-party modules compiled against `protocol: 1` remain compatible with future Genesis core releases.
- If a future breaking change alters the lifecycle wire protocol, `sdk.ProtocolVersion` will be incremented (e.g. to `2`), and modules will be rejected early during resolution with clear diagnostics.
