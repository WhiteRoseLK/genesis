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

### 2026-09-18 — J4 Résolveur, broker, planificateur, moteur
Le jalon le plus complexe à ce jour. Détail complet des décisions dans les messages de commit (`git log --oneline` depuis « feat: function protocols and bidirectional broker plumbing » jusqu'à « feat: wire genesis plan/apply ») ; résumé ici.

**Fait**
- **Fonctions concrètes** : `functions/core/secrets/v1` (natif, jamais un module), `functions/compute/vm/v1` (fournie par `fake-compute`), `functions/test/echo/v1` (générique, réutilisée par les modules de test sous des noms différents).
- **SDK** : `sdk.Serve` accepte des `FunctionProvider` (une fonction fournie = un plugin `function:<nom>` dispensé en plus du cycle de vie) ; `BrokerAware`/`BrokerClient` permettent à un module de dialer, pendant une étape, le sous-canal go-plugin ouvert par le cœur (`StepRequest.broker_token`) — c'est le mécanisme d'appel bidirectionnel (comme les provisioners Terraform), symétrique du sens habituel cœur→module.
- **`internal/broker`** : `Registry.BuildSession(appelant, allowed)` construit un `grpc.Server` n'enregistrant que les fonctions déclarées — une fonction non déclarée n'est simplement jamais enregistrée, l'appel échoue avec `Unimplemented` **architecturalement**, pas via une vérification ad hoc. `core.secrets/v1` a une implémentation native avec contrôle d'accès (owner/consumers, via `secrets.Store.GetMeta`, ajoutée pour l'occasion). `compute.vm/v1` et `test.*/v1` sont relayées vers le module fournisseur actif (`ForwarderFor`).
- **`internal/resolver`** : capacité → module (explicite, seul candidat installé, ou `defaults.<capacité>`), fermeture des fonctions requises jusqu'à point fixe (ajout automatique), compatibilité `core` (comparateur semver maison, pas de nouvelle dépendance). `FunctionProviders` est **par phase** (`fonction → phase → module`) : une fonction peut avoir un fournisseur graine et un fournisseur cible différents en même temps (doc 05), ce n'est pas un conflit.
- **`internal/planner`** : DAG à granularité module (arêtes dérivées des fonctions requises/fournies), tri topologique de Kahn déterministe, détection de cycle.
- **`internal/engine`** : lance tous les modules résolus, enregistre leurs fonctions fournies dans le broker, puis pour chaque module : un seul `Check` décide de rejouer ou non son groupe d'étapes (seed.up si applicable, provision, configure, verify, puis handover+seed.retire si passation) — simplification assumée par rapport à un `Check` par RPC individuel, sûre car chaque étape est elle-même idempotente (doc 03 §4 règle 3). État persisté après chaque étape réussie.
- **Modules réels** : `modules/fake-compute` (registre mémoire, pas de conteneurs systemd/SSH pour ce jalon) ; `test/modules/{test-a,test-b,test-c,test-d}` — test-a exerce graine+passation, test-b/c/d appellent réellement leur dépendance à travers le broker pendant `Verify`.
- **CLI** : `genesis plan`/`genesis apply` branchés pour de vrai (résolution, plan affiché avec modules ajoutés automatiquement marqués, confirmation y/N sauf `--auto-approve`, exécution via le moteur).
- Les 5 critères d'acceptation vérifiés individuellement par leur nom de test (voir session) : ordre correct, cycle détecté, fonction non déclarée refusée, kill+relance→reprise, second apply→0 changement, plus la preuve d'extensibilité avec `test-d`.

**Décisions**
- Bug réel trouvé et corrigé en cours de route : `GRPCBroker.AcceptAndServe` de go-plugin est **bloquant** (comme `http.Server.Serve`) ; l'appeler de façon synchrone dans `OpenSession` gelait tout le moteur dès la première étape. Corrigé en le lançant dans une goroutine — `Dial` côté module attend déjà jusqu'à 5s les infos de connexion, aucune synchronisation supplémentaire nécessaire.
- Généricité du broker sans changement du cœur par module : chaque type de fonction "officiel" (défini une fois dans le SDK) a un forwarder écrit une fois dans `internal/broker` ; ajouter un **module** ne touche jamais ce fichier, seul l'ajout d'une **nouvelle fonction** (rare, au niveau SDK) le ferait — cohérent avec la portée de la règle non négociable.
- Pas de choréographie `Repoint` explicite envoyée aux consommateurs : dans cette architecture, un consommateur rouvre une session de broker à chaque appel de fonction, donc changer le fournisseur actif dans le registre suffit à « repointer » sans RPC dédié. La choréographie complète multi-module du doc 05 (vraie passation DNS/PKI) reste à construire quand les modules réels (J6/J7) en auront besoin.
- `internal/state.State` gagne un champ `Modules` (état opaque par module, `StepResult.state` persisté) — c'est le jalon qui le produit réellement, pas un ajout spéculatif.
- `secrets.Store` gagne `GetMeta` — nécessaire au contrôle d'accès de `core.secrets/v1`, absent jusqu'ici car rien n'en avait besoin.

**Dette**
- `internal/engine` rejoue le groupe d'étapes complet d'un module non conforme plutôt que de reprendre exactement à la RPC en échec — assumé, sûr par idempotence, mais moins granulaire que ce que le doc02 semble impliquer ("apply reprend à la première étape non conforme").
- Choréographie de passation multi-module (Repoint explicite, migration secrets file→vault) non construite : différée à J6/J7 avec les vrais modules DNS/PKI.
- Config résolue de la spec pas encore transmise au module (`StepRequest.config` est toujours vide) : aucun module réel n'en a encore besoin ; à câbler avec proxmox (J5).

**Prochaine étape** : J5 — Modules `proxmox` et `base-os` (doc 07).

### 2026-09-18 — J5 Modules `proxmox` et `base-os`
Premier jalon touchant du terrain non vérifiable dans cet environnement (pas de vrai cluster Proxmox — confirmé avec l'utilisateur avant de commencer) et premier à exercer du contenu Ansible réel (Docker disponible sur cette machine, utilisé pour de vrai).

**Décision de portée (utilisateur)** : `os.base/v1` sépare la configuration fonctionnelle (ce qui est nécessaire au fonctionnement de la VM — CA, résolveur, NTP) du durcissement pur (SSH, nftables — sécurité). Seule la première moitié est construite à ce jalon ; `Harden` est un stub explicite (`Unimplemented`, message clair), pas une fonctionnalité à moitié écrite. Citation : *« le hardening pur tu oublie pour le moment ce sera une feature plus tard [...] tu peux tester tout ce qui est nécessaire au fonctionnement de la vm (dns, ntp, CA, etc.) »*. Par construction, le critère littéral du doc 08 « VM... durcie » n'est donc satisfait que pour son volet fonctionnel, pas sécurité — décision assumée, pas un oubli.

**Fait**
- `internal/runner.ContainerRuntime` : pilote docker/podman en CLI (`seed.container_runtime`), pas de SDK Docker tiers. Testé pour de vrai contre Docker sur cette machine (run bloquant + code de sortie, run détaché + stop + status, montage host).
- `core.container/v1` et `core.ansible/v1` (natives) : `internal/broker/container.go`, `ansible.go`. `RunPlaybook` écrit playbook/inventaire/vars dans un répertoire partagé monté dans un conteneur `willhallonline/ansible`.
- **Bug réel trouvé et corrigé** : le répertoire de travail partagé doit être lisible par tous (`0755`, pas le `0700` par défaut de `MkdirTemp`) — le conteneur ansible tourne sous son propre UID interne, distinct de celui de l'appelant (confirmé ici via un décalage d'espace de noms utilisateur, mais le problème est général, pas spécifique à cet environnement).
- **Test d'intégration réel** (`internal/broker/ansible_test.go`) : conteneur SSH jetable (`linuxserver/openssh-server`) comme cible, playbook exécuté pour de vrai, vérifié via `docker exec` (pas la parole d'ansible). Deux limites d'environnement documentées dans le test : le réseau pont docker0 n'est pas joignable directement depuis le process de test ici (seulement depuis un autre conteneur, ou via le daemon) — vérification via `docker exec`, pas dial direct ; l'image cible n'a pas Python — playbook de test en `ansible.builtin.raw` (les vraies VM cibles, image cloud Debian, en ont un).
- `modules/base-os` : `TrustCA`/`SetResolver`/`SetNTP` réels (playbooks Ansible embarqués), `Harden` stub. **Bug réel #2** : `broker.Dial(token)` ne réussit qu'une fois par jeton de session (les infos de connexion transitent par un canal à usage unique côté cœur) — appeler `TrustCA` puis `SetResolver` en re-dialant le même jeton à chaque fois bloquait le second appel. Corrigé en mettant en cache la connexion dialée une fois, réutilisée pour tous les appels de fonction suivants.
- `modules/proxmox` : client REST Proxmox VE écrit à la main (`proxmoxapi/`), testé contre des fixtures `httptest` fidèles à l'API documentée (pas de SDK tiers — la surface nécessaire est étroite et de toute façon invérifiable en direct ici). `EnsureVM` idempotent par nom, `DeleteVM` idempotent. **Portée assumée** : `EnsureImage` suppose qu'un template existe déjà (pas de téléchargement d'image cloud + conversion automatique — la chaîne la plus complexe et la moins vérifiable sans cluster réel).
- **Bug réel #3, trouvé par le test de fixture lui-même** : le clone Proxmox porte l'ID du *template* dans le chemin d'URL et le *nouvel* ID dans un champ de formulaire `newid`, pas l'inverse — le client (`proxmoxapi`) avait déjà ça correctement, c'est la fixture de test qui s'était trompée ; détecté seulement parce que le test exerce la vraie forme de requête de bout en bout plutôt que de mocker à la frontière de chaque méthode.
- **Gaps J4 comblés en cours de route** (exactement la méthode annoncée par l'utilisateur : « au fur et à mesure des tests, si il manque quelque chose d'obligatoire, on vient le rajouter ») :
  - `resolver.Module.Config` : la config résolue de la spec (doc04, fusionnée si plusieurs capacités pointent vers le même module) est maintenant transmise à `StepRequest.config` — J4 la laissait toujours vide, faute de module qui en ait eu besoin.
  - `internal/engine.resolveRefs` : les champs `*_ref` (`env://`, `file://`, `vault://`) sont résolus en valeurs réelles avant d'atteindre un module (`vault://` : erreur claire « pas encore pris en charge, J7 », pas un échec silencieux).
  - `compute.vm/v1.EnsureVMRequest` gagne `ip`, `gateway`, `ssh_public_key`, `user` (cloud-init, doc07) — ajout additif, pas de rupture.
- Suite de conformité SDK (`moduletest.RunConformance`) câblée pour `proxmox` et `base-os` (les modules écrits à la main n'en ont pas par défaut, contrairement à ceux générés par `scaffold`) — critère d'acceptation explicite du doc 08.

**Dette**
- `EnsureImage` (proxmox) : pas de téléchargement/conversion automatique de template — à construire quand validable contre un vrai cluster (J9 ou demande explicite).
- `Harden` (base-os) : décision de portée ci-dessus, pas encore construit.
- nftables et résolveur/NTP réellement câblés en `Repoint` (pas seulement à l'appel initial) : pas encore exercé — dépend de vrais modules DNS/PKI (J6/J7).
- Allocation d'IP depuis le pool (doc04 `network.pool`) : pas encore construite ; `EnsureVMRequest.ip` est pour l'instant fourni par l'appelant, pas alloué par le cœur.
- `proxmox`/`base-os` jamais exécutés contre une vraie infrastructure — uniquement fixtures HTTP et conteneurs jetables.

**Prochaine étape** : J6 — Modules `chrony`, `coredns`, `powerdns` (doc 07).

### 2026-09-18 — J6 Modules `chrony`, `coredns`, `powerdns`
Premier jalon à exercer une vraie passation multi-module (docs/05-cycle-bootstrap.md) : `coredns` (graine) → `powerdns` (cible) sur les mêmes fonctions `dns.zone/v1`/`dns.resolver/v1`. Le mécanisme n'existait pas encore dans `internal/engine` (seule l'auto-passation d'un module se reprenant lui-même, `test-a`, était exercée) — construit à ce jalon plutôt que deviné à l'avance, conformément à la méthode déjà validée par l'utilisateur (« on vient rajouter au fur et à mesure des tests »).

**Fait**
- `modules/coredns` : `dns.zone/v1` + `dns.resolver/v1` en phase graine, conteneur CoreDNS réel sur la graine (`core.container/v1`), zone BIND régénérée à chaque `UpsertRecord`/`DeleteRecord`. Résolution DNS prouvée pour de vrai (requête depuis un conteneur tiers, pas `docker exec` dans CoreDNS lui-même). Devenu un vrai module piloté par le moteur à l'occasion de ce jalon : `Check` distingue désormais `A_FAIRE`/`CONFORME` (`seeded`/`retired`), `SeedUp` amorce, `SeedDown` arrête réellement le conteneur.
- `modules/chrony` : premier module cible possédant son propre cycle de vie VM complet (`compute.vm.EnsureVM` + paire SSH via `core.secrets`, `Configure` installe/configure `chronyd` serveur via `core.ansible` avec son propre playbook, `Verify` mesure un écart réel (`chronyc tracking`) depuis une VM tierce jetable, seuil < 100 ms, `Destroy` supprime la VM).
- `modules/powerdns` : `dns.zone/v1` + `dns.resolver/v1` en phase cible — Authoritative (SQLite, API) + Recursor sur sa propre VM. `api.go` parle directement à l'API REST de PowerDNS (même principe que `modules/proxmox/proxmoxapi`, pas de SDK tiers). `Configure` consomme pour de vrai `time.ntp/v1` et `dns.resolver/v1` (chrony/coredns, tous deux réels à ce stade — contrairement à chrony qui n'avait encore aucun fournisseur à construire). `Handover` relit `dns.zone/v1@seed` (coredns), recrée chaque enregistrement via sa propre implémentation, compare. `Verify` prouve résolution directe + inverse + nom externe (recursor) depuis une VM tierce jetable.
- **`internal/engine` restructuré pour la passation croisée réelle** : enregistrement des fournisseurs par clé qualifiée (`fonction@phase`) en plus de la clé active (sans suffixe) ; un module graine pur (aucune phase cible, ex. coredns) s'arrête à `seed_ready` (`SeedUp`+`Verify`) au lieu d'essayer `provision`/`configure`/`handover` ; `handoverSeedModules` détecte, par fonction fournie en phase cible, un fournisseur graine actif — même module (auto-passation, `test-a`) ou différent (`coredns`→`powerdns`) — et déclenche `SeedDown` sur **chacun**, dédupliqué ; `repoint` fait du module cible le nouveau fournisseur actif après passation réussie, sans redispense.
- Forwarders broker manquants ajoutés : `os.base/v1`, `dns.zone/v1`, `dns.resolver/v1`, `time.ntp/v1` (seul `compute.vm/v1` en avait un ; aucun module ne consommait encore les autres via le broker réel avant ce jalon).
- Nouveaux fixtures `test/modules/test-e` (graine pure) / `test-f` (cible, lit `test.e/v1@seed`) : preuve, hors produits réels, que le mécanisme de passation croisée route `Handover` vers le bon module et `SeedDown` vers l'**autre** — `internal/engine/handover_test.go`.
- Chaque module testé conformément à la règle 7 du doc 03 (« testable seul, fonctions requises simulées ») : le mécanisme de transport réel (`core.ansible/v1` via Docker) est prouvé une fois pour toutes en J5, chaque module test ensuite son propre câblage avec des fournisseurs simulés — sauf `coredns` (résolution DNS prouvée pour de vrai, peu coûteux) et `powerdns.api.go` (round-trip HTTP réel via `httptest`, indépendant de Docker).

**Décisions**
- ADR-016 : `core.ansible/v1` ouvert à tout module cible, pas réservé à `base-os` — `chrony`/`powerdns` déclarent leur propre playbook produit, `os.base/v1` reste réservé à la configuration commune (CA, résolveur, NTP client).
- Le `Repoint(RepointRequest)` explicite du proto n'est pas encore déclenché par le cœur vers les modules consommateurs : dans ce MVP, aucun module ne consomme encore `dns.zone/v1`/`dns.resolver/v1`/`time.ntp/v1` sans suffixe de phase au-delà de son propre `Check` (le seul consommateur sensible à la passation, `powerdns`, lit explicitement `dns.zone/v1@seed`, une clé stable que la passation ne change jamais). La clé de registre « active » (repoint) est néanmoins mise à jour pour de vrai après chaque passation — l'infrastructure est en place, simplement pas encore observée par un consommateur réel.
- `os.base.TrustCA` reste non appelé par `chrony`/`powerdns` (pas de CA avant `step-ca`/J7) ; `SetNTP`/`SetResolver` en revanche sont maintenant réellement exercés par `powerdns` (chrony et coredns existent déjà à ce stade du jalon).

**Dette**
- Comme `coredns`, les paramètres de connexion de `powerdns` (adresse VM, clé API) vivent en mémoire dans le process du module, peuplés par `Configure` — perdus si le cœur redémarre en cours de cycle de vie (même dette, même justification : `StepResult.state` n'est pas transmis aux gestionnaires de fonction `dns.zone/v1`, seulement aux étapes de cycle de vie).
- Pas de dérivation automatique de zone inverse/PTR : `dns.zone/v1` accepte n'importe quel type d'enregistrement (PTR compris) de façon générique, mais rien ne crée automatiquement la zone `in-addr.arpa` correspondant à un enregistrement A — `Verify` de `powerdns` le fait à la main pour sa propre sonde.
- `Repoint(RepointRequest)` explicite non câblé côté cœur (voir décision ci-dessus) — à construire dès qu'un module consomme une fonction à passation sans en capter le jeton de session à chaque appel (voir aussi la nuance suivante).
- `chrony`/`coredns`/`base-os` mettent en cache leur connexion broker depuis le **premier** jeton reçu (`Check`) et le réutilisent pour tout leur cycle de vie, alors que `internal/engine` fournit un jeton neuf à chaque étape (`test-b`/`test-f` redialent avec le jeton de l'appel en cours, le schéma idiomatique). Sans conséquence ici (aucun de ces modules ne consomme une fonction à passation après son propre `Check`), mais à corriger — dialer avec `req.GetBrokerToken()` à chaque appel, pas un jeton mis en cache — avant qu'un module existant ne consomme une fonction sujette à repoint (`vault`/`openssh-bastion`, J7/J8).
- Aucun des trois modules n'a été exécuté à travers `internal/engine.Run()` avec un vrai cluster Docker multi-module de bout en bout (coredns+chrony+powerdns+fake-compute+base-os ensemble) — chaque module est prouvé isolément (fonctions requises simulées) plus le mécanisme de passation prouvé génériquement (`test-e`/`test-f`). La preuve multi-module réelle est le périmètre explicite de J9 (doc 08).

**Prochaine étape** : J7 — Modules `step-ca`, `vault` (doc 07).

### 2026-09-19 — J7 Modules `step-ca`, `vault`
Le jalon le plus lourd du projet à ce jour, découpé en deux étapes validées séparément par l'utilisateur (step-ca d'abord, point d'étape, puis vault). Premier jalon à exercer une vraie hiérarchie PKI (racine → intermédiaire → intermédiaire tiers) et une migration de secrets orchestrée par le cœur.

**Fait**
- `modules/step-ca` : `pki.issuer/v1` en phase graine — `IssueCert`/`SignCSR`/`CAChain` pilotent réellement le CLI `step` du conteneur `smallstep/step-ca` (mode local/hors-ligne : `step certificate create`/`sign`, pas de serveur réseau détaché — évite toute la complexité TLS/DNS/provisioners du mode API, non nécessaire tant que rien ne consomme l'API HTTP de step-ca elle-même). `SignSSH` : stub `Unimplemented` explicite, différé à `openssh-bastion` (J8), même méthode que `Harden` dans `base-os` (J5).
- **Bug réel trouvé en préparant vault** : la racine step-ca par défaut (`--profile root-ca`, pathlen:1) ne permet de signer qu'un seul niveau d'intermédiaire — insuffisant pour que `vault` obtienne son propre `pki_int` (un intermédiaire de plus dans la chaîne) signé par step-ca. Corrigé : racine générée via template (`--template`, seul moyen de fixer `maxPathLen` sur une racine auto-signée) avec `maxPathLen: 2` ; `SignCSRRequest` gagne `is_ca`/`path_len_constraint` (ajout additif) pour demander un intermédiaire plutôt qu'une feuille.
- `modules/vault` : `pki.issuer/v1` + `secrets.kv/v1` en phase cible — Raft mono-nœud, TLS initial via `pki.issuer/v1@seed` (step-ca signe le certificat serveur de vault), `pki_int` signé par step-ca (exactement le scénario du bug ci-dessus), KV v2, AppRole `genesis`. `api.go` parle directement à l'API REST de Vault (même principe que `modules/powerdns/api.go`, `modules/proxmox/proxmoxapi`).
- `Handover` (vault) : réémet son propre certificat TLS via son propre `pki_int` (pas celui de step-ca), redéploie, révoque le root token (docs07).
- Mécanisme de **migration `file` → `vault`** construit dans `internal/engine` (`migrateSecretsIfNeeded`) : dès que le module choisi pour la capacité `secrets` fournit `secrets.kv/v1` et devient `target_ready`, chaque secret non-`recovery` est copié, vérifié par relecture, puis `state.SecretsBackend` bascule — ce n'est pas un handover de fonction classique (`secrets.kv/v1` n'a pas de fournisseur graine à reprendre, c'est `core.secrets/v1`, natif, qui change de backend), donc un déclencheur séparé de `handoverSeedModules`/`repoint` (J6). Prouvé génériquement avec un nouveau fixture `test-kv` (KV en mémoire), comme `test-e`/`test-f` pour la passation croisée.
- Nouveau proto `secrets.kv/v1` (`Read`/`Write`/`List`) + forwarder broker ; forwarder `pki.issuer/v1` ajouté.
- `step-ca` testé contre le vrai conteneur (émission + signature de CSR réelles, chaîne vérifiée cryptographiquement jusqu'à la racine, idempotence prouvée à travers un redémarrage du module, et signature d'un intermédiaire tiers capable de signer sa propre feuille — le scénario exact dont `vault` a besoin).
- `vault` testé contre un vrai conteneur `hashicorp/vault` et un vrai `step-ca` : Vault a besoin d'un vrai systemd+apt Debian pour son installation réelle (contrairement à chrony/coredns/powerdns, hors de portée des conteneurs Alpine de `fake-compute`) — le faux `core.ansible/v1` du test pilote donc directement Docker avec les VRAIES variables (certificats, `role_id`/`secret_id`) que le module lui transmet, pour exercer pour de vrai tout le reste (init, unseal, `pki_int`, KV, AppRole, Handover, Verify).
- **Quatre bugs réels supplémentaires trouvés en testant `vault` contre un vrai serveur** : délai d'élection Raft après descellement même mono-nœud (« local node not active ») ; policy AppRole sans accès à `pki_int/issue` (`IssueCert` échouait en 403 avec le token AppRole, seulement testé jusque-là avec le root token) ; SAN IP envoyé dans `alt_names` (DNS) au lieu de `ip_sans` (silencieusement ignoré par Vault) ; durée de l'intermédiaire et des feuilles confondues (un émetteur ne peut jamais signer un certificat expirant après lui-même) ; redescellement nécessaire après chaque redémarrage du service (le scellement Vault est toujours en mémoire, jamais persistant, y compris pendant `Handover`).

**Décisions**
- Découpage explicite demandé par l'utilisateur : step-ca complètement construit et testé avant de commencer vault, avec point d'étape entre les deux — contrairement à J6 où chrony/coredns/powerdns avaient été enchaînés sans repasser par l'utilisateur.
- Vault reste **un seul module** portant toutes ses fonctionnalités (PKI intermédiaire, KV, AppRole) — décision explicite de l'utilisateur, pas éclaté en plusieurs modules.
- step-ca pilote le **vrai produit** smallstep/step-ca en conteneur (pas une réimplémentation Go de `crypto/x509`) — décision explicite de l'utilisateur, cohérente avec le reste du projet (coredns/chrony/powerdns pilotent aussi de vrais produits).
- Signature des certificats en mode **local/hors-ligne** (`step certificate sign`, pas `step ca sign` réseau) : évite entièrement la complexité de bootstrap TLS/fingerprint/provisioner du mode serveur de step-ca, qu'aucun consommateur n'exige à ce jalon.

**Dette**
- Aucun renouvellement automatique planifié pour l'intermédiaire step-ca ni les certificats TLS de vault — seulement régénérés/réémis au prochain appel qui les trouve expirés (ou proches de l'expiration pour l'intermédiaire step-ca).
- `secrets.kv/v1.List` (vault) : stub `Unimplemented` — LIST v2 KV nécessite une méthode HTTP non standard, pas encore de consommateur réel (la migration écrit/relit par ref connue, ne liste jamais).
- La policy AppRole `genesis` n'est écrite qu'une fois (idempotent via la présence de `role_id`/`secret_id` stockés) : si son contenu doit évoluer plus tard, rien ne la réécrit automatiquement sur un vault déjà configuré.
- Migration `file` → `vault` prouvée génériquement (`test-kv`) mais jamais exécutée en bout en bout avec le vrai module `vault` dans un run `internal/engine.Run()` complet — périmètre explicite de J9.
- Mise à jour de la note J6 : `vault` a maintenant été construit et ne souffre pas du problème de cache de jeton de session identifié alors (aucune fonction sujette à repoint n'est consommée après son propre `Check`) — la vigilance reste nécessaire pour `openssh-bastion` (J8).

**Prochaine étape** : J8 — Module `openssh-bastion` et retrait de la graine (doc 07).

## Dette technique connue
- Clé maîtresse en fichier local (ADR-007)
- Profil connected uniquement (ADR-008)
- Vérification des licences des dépendances non implémentée en CI (J0, reportée à la demande utilisateur)
- `internal/engine` rejoue le groupe d'étapes complet d'un module non conforme plutôt que reprendre à la RPC exacte en échec (J4, sûr par idempotence mais moins granulaire)
- Passation multi-module (Handover/SeedDown croisé, clé de registre repointée) **construite en J6** ; migration des secrets file→vault **construite en J7** (prouvée génériquement, jamais exécutée en bout en bout avec le vrai module vault).
- `Harden` (base-os) : durcissement pur (SSH, nftables) différé à une itération future, décision utilisateur (J5)
- `EnsureImage` (proxmox) : pas de téléchargement/conversion automatique de template Proxmox, template pré-existant supposé (J5 → à construire quand validable contre un vrai cluster)
- Allocation d'IP depuis le pool réseau (doc04) non construite : IP fournie par l'appelant, pas allouée par le cœur (J5)
- `proxmox`/`base-os` jamais exécutés contre une vraie infrastructure (pas d'accès Proxmox), uniquement fixtures et conteneurs jetables (J5)
- `coredns`/`powerdns` : paramètres de connexion (`dns.zone/v1`) en mémoire dans le process du module, non transmis via `StepResult.state` (J6)
- Pas de dérivation automatique de zone inverse/PTR dans `dns.zone/v1` (J6)
- `Repoint(RepointRequest)` explicite non câblé côté cœur ; `chrony`/`coredns`/`base-os`/`step-ca`/`vault` mettent en cache leur jeton de session depuis leur premier `Check` plutôt que de redialer à chaque appel (`req.GetBrokerToken()`, schéma idiomatique de `test-b`/`test-f`) — sans conséquence tant qu'aucun ne consomme une fonction sujette à passation après son propre `Check` (vérifié pour vault en J7), à corriger avant `openssh-bastion` (J8)
- Aucun test multi-module réel de bout en bout (coredns+chrony+powerdns+fake-compute+base-os+step-ca+vault ensemble via `internal/engine.Run()`) — périmètre explicite de J9
- `pki.issuer/v1.SignSSH` (step-ca, vault) : stub `Unimplemented`, différé à `openssh-bastion` (J8) qui sera le premier consommateur réel (J7)
- `secrets.kv/v1.List` (vault) : stub `Unimplemented`, pas de méthode LIST v2 KV standard, aucun consommateur réel (J7)
- Aucun renouvellement automatique de certificat planifié (intermédiaire step-ca 30 jours, TLS vault) : régénéré/réémis seulement quand un appel le découvre expiré ou proche de l'expiration (J7)
