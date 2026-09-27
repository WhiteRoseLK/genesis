# ADR-001 — Go, binaire unique

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Portabilité (amd64/arm64), déploiement trivial sur une graine, écosystème infra (API Proxmox, Vault, conteneurs). Conséquence : les rôles Ansible et définitions de conteneurs sont embarqués via `go:embed`.
