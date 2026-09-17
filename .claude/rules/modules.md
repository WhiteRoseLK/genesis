---
paths:
  - "modules/**"
  - "test/modules/**"
---
# Règles des modules
- Imports autorisés : `sdk/` et bibliothèques tierces. Jamais `internal/` ni un autre module.
- Le manifest `module.yaml` est la vérité : toute fonction appelée doit figurer dans `requires`, toute fonction servie dans `provides`.
- Pas d'état local : tout ce qui doit persister passe par `StepResult.state`.
- Secrets uniquement via `core.secrets/v1` ; jamais générés ni écrits ailleurs.
- `Verify` teste la fonction réellement, du point de vue d'un consommateur.
- La suite de conformité du SDK doit passer avant de considérer le module terminé.
- Référence : docs/03-contrat-module.md, docs/07-modules-mvp.md.
