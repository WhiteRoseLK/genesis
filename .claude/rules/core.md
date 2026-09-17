---
paths:
  - "internal/**"
  - "cmd/**"
---
# Règles du cœur
- Aucun import de `modules/` ni de SDK produit (Proxmox, PowerDNS, Vault…). Le cœur ne manipule que des manifests, des fonctions et des données opaques.
- Aucune variable globale mutable (le moteur devra paralléliser plus tard).
- Le cœur est seul propriétaire de l'état : écriture atomique, verrou, jamais de valeur secrète (uniquement des `secrets.Ref`).
- Le broker refuse tout appel à une fonction non déclarée dans les `requires` du module appelant.
- Référence : docs/02-architecture.md, docs/06-secrets-etat.md.
