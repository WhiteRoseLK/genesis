# ADR-002 — Orchestrate Ansible instead of re-coding configuration

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Service configuration is a solved problem. Ansible runs in a container so that nothing has to be installed. API calls (PowerDNS, Vault) stay in Go when they carry the handover or verification logic.
