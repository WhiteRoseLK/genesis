# Avancement

| Jalon | Statut | Notes |
|---|---|---|
| J0 — Squelette | fait | |
| J1 — Spec et validation | fait | |
| J2 — Secrets et état | fait | |
| J3 — SDK et hôte de modules | fait | |
| J4 — Résolveur, broker, planificateur, moteur | fait | |
| J5 — proxmox, base-os | fait (partiel, voir notes) | Harden (durcissement pur) différé à une itération future, décision utilisateur |
| J6 — chrony, coredns, powerdns | fait | |
| J7 — step-ca, vault | fait | |
| J8 — teleport, retrait graine | fait | ADR-017/018/019/020 ; repoint SSH vers l'agent différé (#16) |
| J9 — Durcissement, preuve d'extensibilité | à faire | |

## Prochaine étape
J9 — Durcissement et preuve d'extensibilité (doc 08, issue #2).

## Où trouver le reste
- **Dette technique** : [issues `dette-technique`](https://github.com/WhiteRoseLK/genesis/issues?q=is%3Aissue+is%3Aopen+label%3Adette-technique), seule source de vérité (ADR-021).
- **Historique détaillé des jalons** (fait, décisions, bugs trouvés) : [`journal.md`](journal.md).
- **Décisions structurantes** : [`09-decisions.md`](09-decisions.md).
- **Détail de chaque changement** : descriptions des PR et `CHANGELOG.md` (Release Please).

## Mise à jour
En fin de jalon seulement : statut dans le tableau ci-dessus, entrée dans `journal.md`, prochaine étape. Pas dans chaque PR (source de conflits).
