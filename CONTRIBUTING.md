# Contribuer à Genesis

Les règles d'architecture et les règles non négociables sont dans [`CLAUDE.md`](CLAUDE.md) et [`docs/`](docs/) : ce document ne décrit que le processus de développement (ADR-021).

## Environnement

- Go (version de `go.mod`), `make`, Docker ou Podman (tests d'intégration). Les autres outils (`golangci-lint`, `buf`, générateurs protobuf, `govulncheck`, `go-licenses`) sont installés aux versions épinglées par `make tools`, dans `.bin/`.
- Monorepo : chaque module a son propre `go.mod`, complet et utilisable hors du dépôt (ADR-022). L'espace de travail `go.work` sert au développement quotidien. Passez par le `Makefile`, qui parcourt tous les `go.mod`.

| Commande | Rôle |
|---|---|
| `make tools` | Installe les outils de développement épinglés dans `.bin/` |
| `make build` | Compile tous les modules (`CGO_ENABLED=0`, `GOARCH=amd64` ou `arm64`) |
| `make test` | Tests unitaires de chaque `go.mod` : sans réseau ni démon de conteneurs |
| `make test-race` | Mêmes tests avec le détecteur de concurrence (CGO requis pour les tests seulement) |
| `make test-docker` | Tests d'intégration contre de vrais conteneurs (build tag `docker`) |
| `make pull-images` | Tire d'avance, avec reprises, les images épinglées dans le code (limites de débit des registres) |
| `make lint` | `golangci-lint`, règles d'import `depguard` comprises |
| `make mod-check` | Chaque `go.mod` est à jour et compile hors de `go.work` |
| `make proto` | Régénère le code protobuf (`buf generate`) : le code généré est commité |
| `make proto-check` | `buf lint` et code généré à jour (la CI vérifie aussi `buf breaking` contre la branche de base) |
| `make vuln` | Vulnérabilités connues atteignables (`govulncheck`) |
| `make licenses` | Licences des dépendances compatibles avec Apache-2.0 |
| `make e2e` | Bout en bout sur Proxmox : **jamais sans demande explicite** |

## Issues

- Chaque sujet part d'une issue : **Bug**, **Évolution** ou **Dette technique** (modèles dans `.github/ISSUE_TEMPLATE/`).
- Une issue ouverte reçoit `needs-triage`, retiré automatiquement quand un label `priorite:haute|moyenne|basse` est posé.
- **Jalons** (ADR-049) : chaque jalon de `docs/08-jalons.md` a un [milestone](https://github.com/WhiteRoseLK/genesis/milestones) et une issue parente (label `jalon`) ; chaque PR prévue est une sous-issue, rattachée au milestone. Le [GitHub Project](https://github.com/users/WhiteRoseLK/projects) donne la vue tableau et roadmap.
- **Décisions** : un choix d'architecture ou de processus se débat dans une issue **« Décision »** (label `decision`). Une fois tranché, la PR qui l'applique ajoute `docs/adr/NNNN-titre.md` (NNNN = numéro de l'issue, modèle `docs/adr/_modele.md`) et sa ligne dans `docs/09-decisions.md`, puis ferme l'issue.
- **Dette** : une limitation différée volontairement reçoit `dette-technique` ; l'issue est la seule source de vérité pour la dette.

## Branches et PR atomiques

- `main` est protégée : on n'y pousse jamais directement, les checks CI sont obligatoires.
- **Une PR = un sujet.** Un jalon se découpe en plusieurs PR successives, chacune verte et relisible seule.
- Nommage : `<type>/<sujet-court>`, ex. `feat/teleport-agent`, `fix/vault-token-cache`, `docs/adr-021`.
- La PR suit le modèle `.github/PULL_REQUEST_TEMPLATE.md` et lie son issue (`Closes #N`).
- Fusion **en squash** uniquement, branche supprimée après fusion.

## Conventional Commits

Le titre de la PR devient le commit sur `main` : il est validé par la CI et alimente le CHANGELOG.

```text
<type>(<scope>): <description courte>
```

- **Types** : `feat`, `fix`, `perf`, `refactor`, `revert`, `test`, `docs`, `ci`, `build`, `chore`.
- **Scope** (facultatif, libre) : couche du cœur (`engine`, `broker`, `resolver`, `spec`…), `sdk`, `proto`, ou le nom du module (`teleport`, `vault`…). Aucune liste n'est maintenue en CI : ajouter un module ne modifie rien hors de son répertoire.
- Changement cassant (contrat module, format de spec ou d'état, chemins d'import du SDK) : `!` après le scope ou `BREAKING CHANGE:` dans le corps.

## Documentation à tenir à jour dans la même PR

- `docs/adr/` et l'index `docs/09-decisions.md` : une ADR pour toute décision structurante ou nouvelle dépendance lourde, issue de son issue « Décision ».
- Documents de conception concernés (`docs/0x-*.md`) si le contrat, la spec ou le cycle changent ; `docs/10-ajouter-un-module.md` si la procédure d'ajout de module change.
- `docs/PROGRESS.md` n'est mis à jour qu'en fin de jalon ; le bilan du jalon va en commentaire de clôture de son issue parente. Le détail de chaque changement vit dans la description de sa PR et dans le CHANGELOG.

## Sécurité

Ne signalez jamais une vulnérabilité dans une issue publique : voir [`SECURITY.md`](SECURITY.md).

## Releases

[Release Please](https://github.com/googleapis/release-please) tient à jour une PR de release par paquet :

- **cœur** : tag `vX.Y.Z`, `CHANGELOG.md` à la racine, version reportée dans `internal/version` (comparée à la contrainte `core` des modules) ;
- **SDK** : tag `sdk/vX.Y.Z` (convention des sous-modules Go), `sdk/CHANGELOG.md`.

Avant 1.0, un `feat` incrémente le patch et un changement cassant le mineur. Une release mineure change la version comparée à la contrainte `core` des modules : **dans la PR qui introduit une rupture**, relever la borne haute de la contrainte des modules du dépôt (`modules/*/module.yaml` et le modèle de `scaffold`) si la rupture ne les empêche pas de fonctionner avec le nouveau cœur, sinon la PR de release échoue en CI. Les modules de test (`test/modules/*`) n'ont pas de borne haute. La publication de binaires (GoReleaser) viendra plus tard.

## Dépendances

Dependabot propose chaque semaine les mises à jour des actions GitHub et de tous les `go.mod` : une PR par dépendance, qui la met à jour dans tous les modules à la fois. Les images de conteneur sont épinglées par version et empreinte dans le code : leur mise à jour est manuelle pour l'instant.
