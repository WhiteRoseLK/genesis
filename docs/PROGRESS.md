# Avancement

| Jalon | Statut | Notes |
|---|---|---|
| J0 — Squelette | fait | |
| J1 — Spec et validation | à faire | |
| J2 — Secrets et état | à faire | |
| J3 — SDK et hôte de modules | à faire | |
| J4 — Résolveur, broker, planificateur, moteur | à faire | |
| J5 — proxmox, base-os | à faire | |
| J6 — chrony, coredns, powerdns | à faire | |
| J7 — step-ca, vault | à faire | |
| J8 — openssh-bastion, retrait graine | à faire | |
| J9 — Durcissement, preuve d'extensibilité | à faire | |

## Journal
<!-- Une entrée par session : date, jalon, fait, décisions, dette, prochaine étape -->

### 2026-09-17 — J0 Squelette
**Fait**
- Module racine `genesis` (`cmd/genesis` + `internal/cli`) : CLI cobra exposant les 9 commandes du tableau doc 02 (`init`, `modules list/install/verify`, `validate`, `plan`, `apply`, `status`, `secrets list/get`, `seed retire`, `destroy`), chacune en stub "pas encore implémentée (jalon Jx)" tant que le jalon correspondant n'est pas fait.
- Module `genesis/sdk` séparé (`sdk/go.mod`) avec placeholder `sdk/go/doc.go` ; `sdk/proto/buf.yaml` + `buf.gen.yaml` prêts pour le protocole `module/v1` (J3), no-op pour l'instant (aucun `.proto`).
- `go.work` (workspace `.` + `./sdk`).
- `LICENSE` Apache 2.0, `NOTICE`, en-têtes SPDX sur chaque fichier `.go`.
- `Makefile` : `build`, `test`, `lint` (itèrent sur `GO_MODULES := . sdk`, à étendre à mesure des futurs modules), `proto` (no-op tant qu'aucun `.proto`), `e2e` (build tag `integration`, jamais lancé sans demande explicite).
- `.golangci.yml` : règle `depguard` avec deux listes — `internal/`+`cmd/` ne doivent jamais importer `modules/` ; `sdk/` ne doit jamais importer `internal/` ni `cmd/`. **Vérifié manuellement dans les deux sens** (import interdit temporaire, confirmé bloqué par `golangci-lint run`, puis annulé avant commit) : le mécanisme est prouvé, aucune preuve committée (pas de module réel encore pour un test automatisé — viendra en J4 avec `fake-compute`/`test-a..d`).
- `.github/workflows/ci.yml` : jobs `test` (build + test) et `lint` (golangci-lint, règles d'import comprises) sur push/PR GitHub.
- Outillage installé dans l'environnement de dev : Go 1.27.1, golangci-lint 2.13.2, buf 1.73.0, protoc-gen-go, protoc-gen-go-grpc.
- `make build lint test proto` verts.

**Décisions**
- Nom de module Go : `genesis` (racine) / `genesis/sdk` — pas de domaine (ex. `github.com/...`) tant que le dépôt GitHub réel n'est pas créé ; à renommer en une commande le jour venu.
- CI de développement (lint/test) sur **GitHub Actions** ; le CI/CD de déploiement de l'app sera sur **GitLab**, hors périmètre de ce dépôt.
- Règle d'import J0 implémentée via `depguard` (golangci-lint), pas de script maison — réutilise l'outillage lint déjà en place.
- **Vérification des licences des dépendances : retirée du périmètre J0** à la demande explicite de l'utilisateur, alors que le critère existe dans doc 08. Pas d'ADR formel (ce n'est pas un choix d'architecture, juste un report de tâche) — noté ici comme dette pour rester traçable face au critère d'acceptation du doc.

**Dette**
- Vérification des licences des dépendances en CI : non implémentée (voir décision ci-dessus). À faire avant que le dépôt ne devienne public ou que des dépendances lourdes soient ajoutées.
- La règle d'import `modules ↛ internal` n'a été vérifiée qu'à la main (pas de module réel dans le dépôt pour un test automatisé permanent). À sécuriser par un test de conformité automatisé quand `test/modules/` existera (J4).

**Prochaine étape** : J1 — Spec et validation générale (doc 04).

## Dette technique connue
- Clé maîtresse en fichier local (ADR-007)
- Profil connected uniquement (ADR-008)
- Vérification des licences des dépendances non implémentée en CI (J0, reportée à la demande utilisateur)
- Règle d'import `internal/cmd ↛ modules/` vérifiée manuellement seulement, pas encore par un test automatisé permanent (J0 → à consolider en J4)
