# ADR-001 — Go, single binary

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Portability (amd64/arm64), trivial deployment on a seed, infrastructure ecosystem (Proxmox and Vault APIs, containers). Consequence: Ansible roles and container definitions are embedded with `go:embed`.
