---
description: Audit des règles d'architecture modulaire de Genesis
---
Audite le dépôt sans rien modifier :
1. Imports interdits : `internal/` → `modules/` ou bibliothèques produit ; `modules/*` → `internal/` ou autre module ; `sdk/` → `internal/`.
2. Noms de produits ou ordre de couches codés en dur dans le cœur.
3. Fonctions appelées par un module mais absentes de ses `requires`, ou servies sans être dans `provides`.
4. Secrets potentiellement exposés (logs, erreurs, fixtures, état).
5. Écarts entre le code et `docs/03-contrat-module.md`.
Rends un rapport classé par gravité avec fichier et ligne, et une proposition de correction pour chacun.
