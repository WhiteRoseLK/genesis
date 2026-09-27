---
paths:
  - "modules/**"
  - "test/modules/**"
---
# Module rules
- Allowed imports: `sdk/` and third-party libraries. Never `internal/` or another module.
- The `module.yaml` manifest is the source of truth: every function called must be listed in `requires`, every function served in `provides`.
- No local state: anything that must persist goes through `StepResult.state`.
- Secrets only through `core.secrets/v1`; never generated or written anywhere else.
- `Verify` really tests the function, from a consumer's point of view.
- The SDK conformance suite must pass before the module is considered done.
- Reference: docs/03-module-contract.md, docs/07-mvp-modules.md.
