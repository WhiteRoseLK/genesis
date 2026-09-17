# Genesis

Outil open source (Go, Apache 2.0) qui construit automatiquement un socle d'environnement autonome (NTP, DNS, PKI, Vault, bastion…) sur un cluster de virtualisation, à partir d'une spec YAML. Architecture **cœur générique + modules plugins** (HashiCorp go-plugin, gRPC).

## Documentation de conception (source de vérité)
Ne pas tout charger d'emblée : lire le document utile à la tâche en cours.
- `docs/01-vision-perimetre.md` — périmètre et non-objectifs de l'itération 1
- `docs/02-architecture.md` — cœur, modules, fonctions, broker, organisation du dépôt
- `docs/03-contrat-module.md` — manifest, protocole gRPC, fonctions, règles
- `docs/04-spec.md` — format de la spec utilisateur
- `docs/05-cycle-bootstrap.md` — graine → cible → passation → retrait
- `docs/06-secrets-etat.md` — secrets, redaction, état
- `docs/07-modules-mvp.md` — modules de l'itération 1
- `docs/08-jalons.md` — jalons et critères d'acceptation
- `docs/09-decisions.md` — ADR
- `docs/10-ajouter-un-module.md` — procédure d'ajout de module
- `docs/PROGRESS.md` — **avancement : à lire en début de session, à mettre à jour en fin de tâche**

## Méthode de travail
- Implémenter **un jalon à la fois**, dans l'ordre du doc 08. Ne pas anticiper les jalons suivants.
- Avant de coder un jalon : proposer un plan court (fichiers, interfaces, tests) et attendre validation.
- Si un document est ambigu ou contradictoire : poser la question plutôt que deviner ; si une décision structurante est prise, l'ajouter en ADR dans `docs/09-decisions.md`.
- Fin de jalon : critères d'acceptation vérifiés, `make lint test` vert, `docs/PROGRESS.md` à jour, commit par étape logique (Conventional Commits).

## Règles non négociables
- **Aucun secret en clair** dans logs, sorties, état, erreurs, fixtures ou commits. Type `Secret` avec redaction.
- **Idempotence** : `Check` avant toute action ; relancer ne produit aucun changement.
- `internal/` n'importe jamais `modules/` ni une bibliothèque propre à un produit. Aucun `switch` sur un nom de produit, aucun ordre de couche codé en dur.
- `modules/*` n'importe que `sdk/` + bibliothèques tierces. Interactions entre modules uniquement via le broker et les fonctions déclarées dans `requires`.
- Ajouter un module ne doit nécessiter aucune modification hors de son répertoire.
- Plan avant apply ; erreurs actionnables (module, étape, cause, piste).
- Nouvelle dépendance lourde → ADR.

## Stack et commandes
- Go stable, `CGO_ENABLED=0`, cibles linux/amd64 et linux/arm64. `cobra`, `yaml.v3`, `log/slog`, `buf` pour protobuf, `hashicorp/go-plugin`, `filippo.io/age`.
- `make build` · `make test` · `make lint` · `make proto` · `make e2e` (build tag `integration`, nécessite un Proxmox : ne jamais lancer sans demande explicite).
- Tests unitaires sans réseau ; module `fake-compute` pour le bout en bout ; suite de conformité SDK obligatoire pour chaque module.

## Pièges connus
- Ne jamais tester contre un vrai Proxmox ou exécuter `genesis apply`/`destroy` sur une infra réelle sans demande explicite.
- Chaque module a son propre `go.mod` : lancer les commandes Go depuis le bon répertoire ou via le `Makefile` (workspace `go.work`).
