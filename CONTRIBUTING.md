# Contribuer à Genesis

Les règles d'architecture et les règles non négociables sont dans [`CLAUDE.md`](CLAUDE.md) et [`docs/`](docs/) : ce document ne décrit que le processus de développement (ADR-021).

## Environnement

- Go (version de `go.mod`), `make`, `golangci-lint` v2, `buf` (protobuf), Docker (tests des modules contre des conteneurs jetables).
- Monorepo en workspace `go.work` : chaque module a son propre `go.mod`. Passez par le `Makefile`, qui les parcourt tous.

| Commande | Rôle |
|---|---|
| `make build` | Compile le monorepo |
| `make test` | Tests unitaires de chaque `go.mod` (sans réseau) |
| `make test-race` | Mêmes tests avec le détecteur de concurrence (CGO requis pour les tests seulement) |
| `make lint` | `golangci-lint`, règles d'import `depguard` comprises |
| `make proto` | Régénère le code protobuf (`buf generate`) |
| `make e2e` | Bout en bout sur Proxmox : **jamais sans demande explicite** |

## Issues

- Chaque sujet part d'une issue : **Bug**, **Évolution** ou **Dette technique** (modèles dans `.github/ISSUE_TEMPLATE/`).
- Une issue ouverte reçoit `needs-triage`, retiré automatiquement quand un label `priorite:haute|moyenne|basse` est posé.
- Les jalons (`docs/08-jalons.md`) sont suivis par le label `jalon` ; une limitation différée volontairement reçoit `dette-technique` et figure aussi dans `docs/PROGRESS.md`.

## Branches et PR atomiques

- `main` est protégée : on n'y pousse jamais directement.
- **Une PR = un sujet.** Un jalon se découpe en plusieurs PR successives (ex. pour J8 : « fleet.agent/v1 dans le résolveur », « module teleport », « retrait automatique de la graine »), chacune verte et relisible seule.
- Nommage : `<type>/<sujet-court>`, ex. `feat/teleport-agent`, `fix/vault-token-cache`, `docs/adr-021`, `ci/release-please`.
- La PR suit le modèle `.github/PULL_REQUEST_TEMPLATE.md`, lie son issue (`Closes #N`) et le jalon concerné.
- CI obligatoire : `build & test`, `tests (détecteur de concurrence)`, `lint`, `Validate Title`.
- Fusion **en squash** uniquement, branche supprimée : `gh pr merge <N> --squash --delete-branch`.

## Conventional Commits

Le titre de la PR devient le commit sur `main` : il est validé par la CI et alimente le CHANGELOG.

```text
<type>(<scope>): <description courte>
```

- **Types** : `feat`, `fix`, `perf`, `refactor`, `revert`, `test`, `docs`, `ci`, `build`, `chore`.
- **Scopes** (facultatifs) : couches du cœur (`cli`, `spec`, `secrets`, `state`, `resolver`, `planner`, `engine`, `broker`, `runner`, `modulehost`, `scaffold`), `sdk`, `proto`, `modules`, `deps`, `ci`, `release`, `main`.
- Un module s'écrit avec le scope générique `modules` et son nom dans la description, ex. `feat(modules): teleport enrôle l'agent SSH`. Aucun nom de module n'est listé dans la CI : ajouter un module ne modifie rien hors de son répertoire.
- Changement cassant (contrat module, format de spec ou d'état) : `!` après le scope ou `BREAKING CHANGE:` dans le corps.

## Documentation à tenir à jour dans la même PR

- `docs/PROGRESS.md` : fait, décisions, dette, prochaine étape.
- `docs/09-decisions.md` : une ADR pour toute décision structurante ou nouvelle dépendance lourde.
- Documents de conception concernés (`docs/0x-*.md`) si le contrat, la spec ou le cycle changent ; `docs/10-ajouter-un-module.md` si la procédure d'ajout de module change.

## Releases

[Release Please](https://github.com/googleapis/release-please) tient à jour une PR de release (`CHANGELOG.md` + version `0.x` tant que l'itération 1 n'est pas close). Fusionner cette PR crée le tag et la release GitHub. La publication de binaires (GoReleaser, cœur et modules) viendra plus tard.

## Dépendances

Dependabot propose chaque semaine les mises à jour des actions GitHub et de tous les `go.mod` (motifs `modules/*`, `test/modules/*`).
