---
paths:
  - "sdk/**"
---
# Règles du SDK
- Toute modification d'une API de fonction existante est une rupture potentielle : ajout de champs uniquement, sinon nouvelle version (`v2`) servie en parallèle.
- Lancer `make proto` après modification d'un `.proto` et committer le code généré.
- Le SDK ne dépend jamais de `internal/`.
- Référence : docs/03-contrat-module.md.
