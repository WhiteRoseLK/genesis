# Avancement

| Jalon | Statut | Suivi |
|---|---|---|
| J0 — Squelette | fait | [journal](journal.md) |
| J1 — Spec et validation | fait | [journal](journal.md) |
| J2 — Secrets et état | fait | [journal](journal.md) |
| J3 — SDK et hôte de modules | fait | [journal](journal.md) |
| J4 — Résolveur, broker, planificateur, moteur | fait | [journal](journal.md) |
| J5 — proxmox, base-os | fait (partiel) | Harden différé ([#7](https://github.com/WhiteRoseLK/genesis/issues/7)) |
| J6 — chrony, coredns, powerdns | fait | [journal](journal.md) |
| J7 — step-ca, vault | fait | [journal](journal.md) |
| J8 — teleport, retrait graine | fait | ADR-017 à 020 ; repoint SSH différé ([#16](https://github.com/WhiteRoseLK/genesis/issues/16)) |
| J9 — Durcissement, preuve d'extensibilité | **en cours** | [milestone J9](https://github.com/WhiteRoseLK/genesis/milestone/1) · issue [#2](https://github.com/WhiteRoseLK/genesis/issues/2) |

## Prochaine étape
J9 : voir les sous-issues de [#2](https://github.com/WhiteRoseLK/genesis/issues/2) et le [milestone J9](https://github.com/WhiteRoseLK/genesis/milestone/1).

## Où trouver le reste (ADR-049)
- **Jalon en cours** : milestone GitHub + issue parente `jalon` + une sous-issue par PR.
- **Dette technique** : [issues `dette-technique`](https://github.com/WhiteRoseLK/genesis/issues?q=is%3Aissue+is%3Aopen+label%3Adette-technique).
- **Décisions** : [`09-decisions.md`](09-decisions.md) (index) et [`adr/`](adr/) ; débats dans les [issues `decision`](https://github.com/WhiteRoseLK/genesis/issues?q=is%3Aissue+label%3Adecision).
- **Historique** : [`journal.md`](journal.md) pour J0 à J8 ; ensuite, commentaire de clôture de l'issue parente de chaque jalon.
- **Détail de chaque changement** : descriptions des PR et [`CHANGELOG.md`](../CHANGELOG.md).

## Mise à jour
En fin de jalon seulement : statut dans le tableau ci-dessus, bilan en commentaire de clôture de l'issue parente, milestone fermé.
