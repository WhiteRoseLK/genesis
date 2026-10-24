---
name: check-architecture
description: "Read-only audit of Genesis's modular architecture rules (forbidden imports, hard-coded products, requires/provides gaps, exposed secrets, contract drift). Use for /check-architecture or when the user asks for an architecture audit."
disable-model-invocation: true
---
Audit the repository without changing anything:
1. Forbidden imports: `internal/` → `modules/` or product libraries; `modules/*` → `internal/` or another module; `sdk/` → `internal/`.
2. Product names or layer order hard-coded in the core.
3. Functions called by a module but missing from its `requires`, or served without being in `provides`.
4. Potentially exposed secrets (logs, errors, fixtures, state).
5. Gaps between the code and `docs/03-module-contract.md`.
Return a report sorted by severity, with file and line, and a proposed fix for each item.
