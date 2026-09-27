---
paths:
  - "sdk/**"
---
# SDK rules
- Any change to an existing function API is a potential breaking change: add fields only, otherwise a new version (`v2`) served alongside.
- Run `make proto` after changing a `.proto` file and commit the generated code.
- The SDK never depends on `internal/`.
- Reference: docs/03-module-contract.md.
