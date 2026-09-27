# ADR-012 — Modules as separate processes via HashiCorp go-plugin (gRPC)

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Alternatives: Go packages compiled into the binary (simpler, but drifts quickly towards a monolith, and every addition needs a recompile); native Go `plugin` plugins (fragile, identical toolchain versions required). Choice: go-plugin, proven by Terraform, Packer and Vault. Benefits: fault isolation, independent versioning, modules writable in other languages through the protobuf definitions. Cost: negligible latency here, protobuf tooling.
