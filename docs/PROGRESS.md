# Avancement

| Jalon | Statut | Notes |
|---|---|---|
| J0 — Squelette | fait | |
| J1 — Spec et validation | fait | |
| J2 — Secrets et état | fait | |
| J3 — SDK et hôte de modules | fait | |
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

### 2026-09-17 — J1 Spec et validation générale
**Fait**
- `internal/spec` : types Go (`Environment`, `Metadata`, `Network`, `Seed`, `Size`, `Capability`), `config` de chaque capacité laissé opaque (`map[string]any`) — sa validation propre au module est déléguée à J3/J4.
- `schema.json` (embarqué, draft 2020-12) valide l'enveloppe générale : champs requis, `apiVersion`/`kind` en `const`, `profile` en `enum: [connected]` (ADR-008), tailles, capacités (`minProperties: 1`), placement ; `additionalProperties: false` partout où la forme est fermée.
- `refs.go` : toute clé `*_ref` doit valoir `env://`, `file://` ou `vault://` (doc 04, refus des secrets littéraux), vérifié récursivement.
- `defaults.go` : `profile=connected`, `seed.state_dir=/var/lib/genesis`, `seed.container_runtime=auto`.
- `Load(path)` : lecture → décodage générique → aller-retour JSON (types natifs attendus par `jsonschema`) → validation schéma (erreurs agrégées, chemin `a.b.c`, avec un traitement particulier des erreurs `missing properties` pour que le chemin pointe le champ manquant plutôt que son parent) → validation `_ref` → décodage typé → défauts.
- `genesis validate -f` exécute cette validation structurelle (résolution des modules et `Validate` par module restent des stubs J3/J4).
- Tests : les deux exemples du doc 04 (complet et minimal) chargent sans erreur ; 13 fixtures invalides (`testdata/invalid/`) échouent chacune avec le chemin YAML attendu dans le message.

**Décisions**
- ADR-015 : `santhosh-tekuri/jsonschema/v5` pour la validation JSON Schema (réutilisable tel quel pour les `config_schema` de module en J3).

**Dette**
- Aucune nouvelle. La validation `config_schema` par module et la résolution capacité→module restent hors périmètre J1, comme prévu par le doc 08.

**Prochaine étape** : J2 — Secrets et état (doc 06).

### 2026-09-17 — J2 Secrets et état
**Fait**
- `internal/secrets` : `Secret` (redaction `String`/`MarshalJSON`/`LogValue`), `Ref` (validation anti-traversal), interface `Store` (doc 06), générateurs (mot de passe 32 car., token 256 bits, clé ECDSA P-384, clé Ed25519, paire SSH Ed25519 — générateur de certificat différé, `pki.issuer` n'existe pas avant J7), `MasterKeyProvider` (ADR-007) + implémentation `file` (identité X25519 `filippo.io/age` dans `state_dir/master.key`, générée une fois par `init`), `FileStore` (secret chiffré age + métadonnées en clair séparées, écritures atomiques, permissions `0700`/`0600`).
- `RedactingHandler` : enveloppe n'importe quel `slog.Handler`, redacte les valeurs `Secret` et les motifs connus (tokens Vault, blocs PEM) dans le message et les attributs, y compris imbriqués.
- `internal/state` : `State{SchemaVersion, SecretsBackend}` — volontairement minimal, le reste (VM, statuts de module, endpoints...) arrivera avec les jalons qui le produisent. `Load/Save` atomiques ; `Lock` via `flock` non bloquant sur `state_dir/state.lock`.
- CLI : flag persistant `--state-dir` (défaut `/var/lib/genesis`) ; `genesis init` génère/charge la clé maîtresse (affichée une seule fois), idempotent ; `genesis secrets list` (métadonnées seulement) et `genesis secrets get <ref>` (révèle la valeur — c'est le but de la commande) branchés sur le backend `file`.
- Tests : `Ensure` idempotent (secrets) ; deux `Lock` concurrents sur le même `state_dir` → le second échoue immédiatement (deux descripteurs distincts, simule fidèlement deux process) ; test de redaction (token Vault, bloc PEM, valeur `Secret`, y compris via `logger.With` et message formaté) — rien ne fuite, tout vert.

**Décisions**
- `--state-dir` en flag persistant sur la racine (absent du tableau CLI du doc 02, mais nécessaire : `init` doit savoir où travailler avant qu'une spec existe) — même défaut que `seed.state_dir` en J1.
- `secrets get` révèle la valeur en clair sur la sortie standard : c'est la fonction même de la commande (récupération déclarée par l'opérateur), à distinguer de la règle « aucun secret en clair » qui vise les fuites non désirées (logs, état, erreurs, `secrets list`).
- Verrou d'état implémenté avec `syscall.Flock` (stdlib, cible Linux uniquement per doc 01) plutôt qu'une dépendance externe.

**Dette**
- Aucune nouvelle. Générateur de certificat (`pki.issuer`) différé à J7 comme prévu, la fonction n'existe pas avant.

**Prochaine étape** : J3 — SDK et hôte de modules (doc 03).

### 2026-09-17 — J3 SDK et hôte de modules
**Fait**
- `sdk/proto/module/v1/module.proto` : service `Module` (11 RPC du doc 03), `config`/`state` en `google.protobuf.Struct` (JSON opaque natif). Package `module.v1` (sans préfixe `genesis.`) pour que le répertoire corresponde à la convention buf. Trois règles STANDARD exceptées dans `buf.yaml` (`SERVICE_SUFFIX`, `RPC_REQUEST_RESPONSE_UNIQUE`, `RPC_REQUEST/RESPONSE_STANDARD_NAME`) : le doc fixe délibérément un service `Module` (pas `ModuleService`) et `StepRequest`/`StepResult` partagés par tout le cycle de vie. `make proto` génère réellement du code désormais.
- `sdk/go` : `Serve(modulev1.ModuleServer)` (go-plugin, handshake partagé `sdk.Handshake`/`sdk.ClientPlugins()`), `LoadManifest`/`ParseManifest`/`ManifestFile.ToProto()` (parsing `module.yaml`, formes courte/longue de `requires`).
- `sdk/go/moduletest` : `RunConformance` — squelette (Describe cohérent avec le manifest, Validate ne plante pas). Suite sémantique complète (idempotence rejouée, secrets absents des sorties, `UpsertRecord`→`ListRecords`) différée à J4 (a besoin du broker).
- `internal/modulehost` : `Discover` (répertoires de recherche, layout `<nom>/<version>/{module.yaml, module-<os>-<arch>, assets/}`), `Fingerprint` (SHA-256), `Launch`/`Client` (go-plugin, logger en Warn) avec `WrapModuleError` pour transformer un crash de module en erreur propre.
- `internal/modulelock` : `genesis.lock` (nom → {version, sha256}), `Verify` refuse une empreinte divergente.
- `internal/scaffold` : `Generate` écrit `modules/<nom>/` complet (go.mod, main.go avec stubs de chaque RPC, module.yaml, schema.json, test de conformité), `go work use`, valide par `go build` — **sans** `go mod tidy` (voir décision ci-dessous).
- CLI : `genesis modules list/install/verify/scaffold` implémentées (plus stubs).
- `test/modules/panicking` : module de test dont `Check` panique, utilisé pour le test de supervision et pour la chaîne scaffold→install→Discover→Launch→Describe de bout en bout.
- Correction du Makefile (J0) : `GO_MODULES` découvert via `find . -name go.mod` au lieu d'une liste codée en dur — sinon chaque module ajouté aurait demandé une édition du Makefile, violation de la règle non négociable « ajouter un module ne doit nécessiter aucune modification hors de son répertoire ».
- Nouvelle règle `depguard` `modules-must-not-import-core` (voir décision ci-dessous).
- `internal/atomicfile` extrait de `internal/secrets`/`internal/state` (règle des trois : `internal/modulelock` en avait besoin aussi).

**Décisions**
- **Pas de `require genesis/sdk` explicite dans le go.mod d'un module, pas de `go mod tidy` automatique.** Sous `go.work`, un `require <module-du-monorepo-sans-domaine-réel> vX` pousse `go build`/`go mod tidy` à tenter une résolution réseau (« malformed module path » ou tentative de fetch DNS) plutôt que d'utiliser la substitution d'espace de travail — vérifié empiriquement (voir historique de session). Sans cette ligne, l'import d'un paquet appartenant à un autre module `use`'d se résout localement sans réseau ni `go.sum`, comportement documenté de `go.work`. `scaffold`/`install` valident donc par `go build` uniquement.
- `genesis/sdk` reste nommé sans domaine réel (cf. décision J0) ; le point ci-dessus est la conséquence concrète de ce choix, maintenant bien comprise.
- `modules-must-not-import-core` (depguard) : très largement redondante avec les règles natives de Go (un paquet sous `internal/` est inimportable hors de l'arborescence du module qui le possède ; `cmd/genesis` est `package main`, jamais importable) — vérifié empiriquement dans les deux cas, l'erreur remontée est `typecheck`, pas `depguard`. Gardée quand même comme documentation/filet de sécurité si un futur paquet du cœur sortait un jour d'`internal/`.
- `genesis modules install` compile le module depuis son répertoire source (`go build`) plutôt que d'attendre un binaire déjà construit — correspond à l'exemple du doc 10 (`genesis modules install ./modules/<nom>`).
- `--lock-file` (défaut `genesis.lock`) ajouté aux commandes `modules install`/`verify` : `genesis.lock` vit « à côté de la spec » (doc 02) mais aucune commande n'a encore de notion de répertoire de travail de spec avant J4.

**Dette**
- Aucune nouvelle latente : le générateur de certificat et la suite de conformité sémantique complète étaient déjà notés comme différés (J7/J4) dans les jalons précédents.

**Prochaine étape** : J4 — Résolveur, broker, planificateur, moteur (docs 02, 05).

## Dette technique connue
- Clé maîtresse en fichier local (ADR-007)
- Profil connected uniquement (ADR-008)
- Vérification des licences des dépendances non implémentée en CI (J0, reportée à la demande utilisateur)
- Règle d'import `internal/cmd ↛ modules/` vérifiée manuellement seulement, pas encore par un test automatisé permanent (J0 → à consolider en J4)
