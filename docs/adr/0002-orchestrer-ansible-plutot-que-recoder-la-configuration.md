# ADR-002 — Orchestrer Ansible plutôt que recoder la configuration

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

La configuration de services est un problème résolu. Ansible exécuté dans un conteneur pour n'imposer aucune installation. Les appels d'API (PowerDNS, Vault) restent en Go quand ils portent la logique de passation ou de vérification.
