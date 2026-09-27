# ADR-013 — Monorepo, artefacts séparés

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Cœur, SDK et modules officiels dans un même dépôt pour itérer vite, mais chacun avec son `go.mod` et son binaire. Les modules pourront migrer vers des dépôts dédiés sans changement de contrat.
