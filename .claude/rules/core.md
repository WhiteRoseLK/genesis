---
paths:
  - "internal/**"
  - "cmd/**"
---
# Core rules
- No import of `modules/` or of any product SDK (Proxmox, PowerDNS, Vault…). The core only handles manifests, functions and opaque data.
- No mutable global variable (the engine will have to parallelise later).
- The core alone owns the state: atomic writes, lock, never a secret value (only `secrets.Ref`).
- The broker refuses any call to a function not declared in the calling module's `requires`.
- Reference: docs/02-architecture.md, docs/06-secrets-state.md.
