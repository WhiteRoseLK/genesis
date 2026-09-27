# 09 — Registre des décisions (ADR)

Format : contexte → décision → conséquences. Statut : `acceptée`, `à confirmer`, `remplacée`.

## ADR-001 — Go, binaire unique — acceptée
Portabilité (amd64/arm64), déploiement trivial sur une graine, écosystème infra (API Proxmox, Vault, conteneurs). Conséquence : les rôles Ansible et définitions de conteneurs sont embarqués via `go:embed`.

## ADR-002 — Orchestrer Ansible plutôt que recoder la configuration — acceptée
La configuration de services est un problème résolu. Ansible exécuté dans un conteneur pour n'imposer aucune installation. Les appels d'API (PowerDNS, Vault) restent en Go quand ils portent la logique de passation ou de vérification.

## ADR-003 — Pas de Terraform/OpenTofu dans l'itération 1 — acceptée
La fonction `compute.vm/v1` est minimale (quelques opérations VM) ; une dépendance à Terraform ajouterait un second état à réconcilier. À réévaluer pour les providers riches (vSphere, cloud).

## ADR-004 — Proxmox comme premier module compute — acceptée
Accès facile en lab, communauté open source large. Nutanix envisagé en second.

## ADR-005 — Capacités décrites par fonctions fournies graine/cible — acceptée
Résout les dépendances circulaires du bootstrap et prépare la couche physique (la virtualisation deviendra une capacité).

## ADR-006 — age pour le backend de secrets local — acceptée
Format simple, bibliothèque Go native, pas de dépendance GPG.

## ADR-007 — Clé maîtresse en fichier local — acceptée (dette)
Suffisant pour valider le fonctionnement. Interface `MasterKeyProvider` obligatoire pour TPM / Shamir / HSM ultérieurs.

## ADR-008 — Profil `connected` uniquement en itération 1 — acceptée (dette)
Les images, paquets et conteneurs sont téléchargés depuis Internet. Toute URL de téléchargement doit néanmoins passer par un composant `artifacts` unique pour permettre le mode air-gap sans refonte.

## ADR-009 — Licence Apache 2.0 — acceptée
Licence permissive : utilisation, modification et intégration commerciale autorisées, y compris par des intégrateurs, avec obligation de conserver la notice et clause de brevets protégeant contributeurs et utilisateurs. Alternative écartée : AGPL-3.0, qui protège contre une reprise en SaaS fermé mais freine l'adoption en entreprise. Conséquences : fichier `LICENSE` Apache 2.0 et fichier `NOTICE` à la racine, en-tête SPDX `// SPDX-License-Identifier: Apache-2.0` dans chaque fichier source, dépendances à licence compatible uniquement (pas de GPL/AGPL dans le binaire).

## ADR-010 — Nom du projet : Genesis — acceptée
Binaire et CLI : `genesis`. Module Go, image et dépôt à nommer en tenant compte des homonymes existants (ex. `genesis-bootstrap` si `genesis` est pris).

## ADR-011 — Architecture cœur + modules, pas de monolithe — acceptée
Contexte : ajouter des produits et, plus tard, la couche physique sans refonte. Décision : cœur générique ; un module par produit ; interactions uniquement via des fonctions versionnées routées par un broker ; ordre de construction dérivé des dépendances, aucune couche codée en dur. Conséquences : SDK à maintenir, discipline de versionnement des API de fonctions, suite de conformité obligatoire.

## ADR-012 — Modules en processus séparés via HashiCorp go-plugin (gRPC) — acceptée
Alternatives : packages Go compilés dans le binaire (plus simple mais dérive rapide vers le monolithe, recompilation pour tout ajout) ; plugins Go natifs `plugin` (fragiles, versions de toolchain identiques exigées). Choix : go-plugin, éprouvé par Terraform, Packer et Vault. Avantages : isolation des pannes, versionnement indépendant, modules écrivables dans d'autres langages via le protobuf. Coût : latence négligeable ici, outillage protobuf.

## ADR-013 — Monorepo, artefacts séparés — acceptée
Cœur, SDK et modules officiels dans un même dépôt pour itérer vite, mais chacun avec son `go.mod` et son binaire. Les modules pourront migrer vers des dépôts dédiés sans changement de contrat.

## ADR-014 — Ports déclarés dans les manifests — acceptée
Permet la co-location de modules sur une VM avec détection de conflits dès `validate`.

## ADR-015 — `santhosh-tekuri/jsonschema/v5` pour la validation JSON Schema — acceptée
Contexte : le doc 04 exige une validation structurelle de la spec par JSON Schema (J1), et le doc 03 prévoit qu'un module publie son `config_schema` en JSON Schema (J3) — les deux ont besoin du même mécanisme de validation. Alternatives : validation Go écrite à la main (pas de dépendance, mais logique dupliquée entre l'enveloppe générale et chaque `config_schema` de module, et divergence probable des messages d'erreur) ; `xeipuuv/gojsonschema` (moins maintenu, draft-4 uniquement). Décision : `santhosh-tekuri/jsonschema/v5`, pur Go, support draft 2020-12, pas de cgo. Conséquence : dépendance ajoutée au module racine (`internal/spec`) dès J1, réutilisée telle quelle par le cœur en J3 pour valider les `config_schema` des modules.

## ADR-016 — `core.ansible/v1` ouvert à tout module cible, pas réservé à `base-os` — acceptée
Contexte : `chrony` (jalon J6) doit installer et configurer `chronyd` en mode serveur, une configuration propre au produit que le contrat générique `os.base/v1` (`Harden`, `TrustCA`, `SetResolver`, `SetNTP`) ne couvre pas. Le doc 07 ne liste `core.ansible/v1` que dans les `requires` de `base-os`, mais ne l'exclut pas des autres modules cible ; le doc 03 le décrit comme une fonction cœur générique, disponible à toute étape déclarant la capacité en `requires`. Alternatives : étendre `os.base/v1` avec une RPC générique de type `RunPlaybook` que les autres modules délégueraient à `base-os` (rejetée : ferait de `base-os` un intermédiaire obligatoire pour toute configuration cible, et coupleraient son contrat à des besoins spécifiques à chaque produit, à l'opposé du principe de fonctions génériques par domaine). Décision : tout module de phase cible peut déclarer `core.ansible/v1` dans ses `requires` et exécuter son propre playbook, exactement comme `base-os` le fait pour la configuration commune — `os.base/v1` reste réservé à la configuration fonctionnelle commune à toute VM (CA, résolveur, NTP client), chaque produit gérant sa propre installation/configuration via son propre playbook embarqué. Conséquence : `chrony` (et les modules cible suivants qui en ont besoin, ex. `powerdns`) déclarent `core.ansible/v1` en plus de `compute.vm/v1` et `os.base/v1`, au-delà de la liste minimale du doc 07.

## ADR-017 — Modules « de parc » : une fonction peut avoir plusieurs fournisseurs actifs simultanés — acceptée
Contexte : `internal/resolver`/`internal/broker` supposent jusqu'ici qu'une fonction a exactement **un** fournisseur actif à la fois (choisi explicitement ou par défaut, repointable lors d'une passation graine → cible, doc05). Ce modèle convient à `dns.zone`, `time.ntp`, `pki.issuer` : un seul CoreDNS ou PowerDNS actif, jamais les deux. Le jalon J8 remplace le bastion OpenSSH classique par **Teleport en mode agent complet** (décision utilisateur) : sa présence dans la spec doit affecter *toute* VM du parc (chaque module qui provisionne une VM doit y déployer l'agent Teleport), et plusieurs modules de ce type peuvent coexister (Teleport et, plus tard, un agent de supervision/journalisation) — contrairement à DNS/NTP/CA, ce n'est pas un choix exclusif entre produits concurrents mais un ensemble de modules tous actifs en même temps. Le projet construit toujours une infrastructure entière de zéro, jamais un ajout après coup sur un parc déjà en production (confirmé explicitement par l'utilisateur) : le planificateur DAG classique résout donc déjà l'ordre de construction (un module « de parc » comme Teleport est construit avant tout module qui en dépend, comme n'importe quelle autre fonction requise) — aucun mécanisme réactif/événementiel n'est nécessaire pour des VM « ajoutées plus tard », qui n'existent pas dans ce modèle.

Alternatives : (a) le module « de parc » découvre lui-même les VM déjà provisionnées (nouvelle fonction `compute.vm/v1.ListVMs`) et s'y connecte avec sa propre clé SSH, déployée comme clé autorisée supplémentaire sur chaque VM — rejetée : complexifie le modèle de secrets (clés aujourd'hui strictement isolées par owner/consumers) sans bénéfice, puisque le problème qu'elle résoudrait (VM ajoutées après coup) ne se pose pas ici. (b) Étendre `os.base/v1` avec la connaissance explicite de Teleport — rejetée pour les mêmes raisons qu'ADR-016 (coupler un contrat générique à un produit précis).

Décision : une fonction peut être déclarée « à fournisseurs multiples » (par opposition au mode par défaut « fournisseur actif unique ») — tous les modules installés qui la fournissent sont **tous** appelés, avec le même `target` que l'appelant utilise déjà pour ses propres appels (`os.base`, `core.ansible`) : jamais besoin de partager une clé SSH entre modules. Pour J8 : nouvelle fonction `fleet.agent/v1` (`Install(target)`), fournie par `teleport`. Chaque module qui provisionne une VM (`chrony`, `powerdns`, `vault`, et tout futur module) déclare `fleet.agent/v1` dans ses `requires` et l'appelle depuis `Configure`, comme il appelle déjà `TrustCA`/`SetResolver`/`SetNTP` — le résolveur garantit que tous les fournisseurs de `fleet.agent/v1` sont construits avant lui (DAG classique). Si aucun module « de parc » n'est installé, l'appel est un no-op (zéro fournisseur).

Conséquence : `internal/resolver`/`internal/broker` gagnent un second mode de résolution (« diffusion vers N fournisseurs » en plus de « fournisseur actif unique, repointable »). `chrony`/`powerdns`/`vault` (déjà écrits) sont modifiés une fois pour ajouter cet appel générique — coût ponctuel, pas répété pour chaque futur module « de parc » ajouté ensuite (supervision, journalisation…).

## ADR-018 — `core.ansible/v1` par certificat OpenSSH, pas par `tsh` — acceptée
Contexte : une fois l'agent Teleport (`ssh_service`) installé sur une VM (J8), sshd natif doit être coupé pour que « SSH direct refusé » (doc08) soit vrai — ce qui casserait les futurs appels `core.ansible/v1` (connexion SSH directe avec la clé privée de service) vers cette VM si le cœur ne bascule pas lui-même vers un chemin passant par Teleport (doc07 : « le cœur bascule ses propres runners SSH en ProxyJump par le bastion »). Décision initiale envisagée : `internal/runner` apprend un `ProxyCommand tsh proxy ssh`, ce qui suppose le binaire `tsh` disponible dans le conteneur qui exécute Ansible (`willhallonline/ansible`, qui ne l'a pas) — implique soit une image Ansible personnalisée, soit un montage du binaire `tsh` depuis une image Teleport à chaque appel.

Vérifié manuellement dans Docker : le service `ssh_service` de Teleport (le node/agent) parle **SSH standard** sur son propre port (3022) — pas un protocole propriétaire. Un client OpenSSH classique (pas `tsh`) s'y connecte directement avec un certificat utilisateur signé par la CA Teleport (`tctl auth sign --format=openssh`), au format `<clé>-cert.pub` qu'OpenSSH reconnaît nativement à côté de la clé privée. `tsh`/le proxy multiplexé (3080) ne sont nécessaires que pour joindre un node non accessible directement au réseau (accès depuis l'extérieur via tunnel inversé) — non pertinent ici, toutes les VM du parc sont sur le même réseau privé.

Décision : `ansiblev1.Target` (`core.ansible/v1`) gagne un champ optionnel `ssh_certificate_pem` (ajout additif) ; quand renseigné, le certificat est écrit à côté de la clé privée au format `<keyfile>-cert.pub` avant de lancer le conteneur Ansible existant, sans aucune modification d'image. Conséquence : pas de dépendance à `tsh` dans le pipeline Ansible, changement minimal du cœur pour porter le « Repoint SSH » du doc07.

## ADR-019 — Repoint SSH vers l'agent Teleport différé hors J8 — acceptée
Contexte : doc08 exigeait pour J8 « SSH direct refusé ». Couper le sshd natif dans `fleet.agent/v1.Install` casserait tout appel `core.ansible/v1` ultérieur vers la VM (ex. `Handover` de `vault`) tant que les appelants n'utilisent pas le certificat Teleport sur le port 3022. Le mécanisme côté cœur existe (ADR-018 : `ansiblev1.Target.ssh_certificate_pem`, géré par `internal/broker`), mais personne ne l'alimente : il faudrait qu'un module obtienne un certificat utilisateur (`access.ssh/v1.SignUserKey`, aujourd'hui `Unimplemented`) et bascule sa cible (port, certificat) après l'enrôlement de l'agent — chantier transverse à tous les modules cible. Alternatives : (a) tout faire dans J8 — rejetée par l'utilisateur, périmètre trop large pour le jalon ; (b) couper sshd sans repoint — rejetée, casse les appels ultérieurs.

Décision (utilisateur) : `fleet.agent/v1.Install` installe et enrôle l'agent Teleport mais **laisse le sshd natif actif** ; `core.ansible/v1` continue à se connecter par clé sur le port 22. `Verify(teleport)` prouve une connexion SSH réelle via l'agent (certificat `tctl auth sign`), pas le refus de l'accès direct. Critère J8 ajusté (doc08), dette tracée par une issue dédiée.

Conséquence : J8 livre un Teleport fonctionnel sur tout le parc sans durcissement d'accès ; le repoint (alimentation de `ssh_certificate_pem`, `SignUserKey`, coupure sshd) reste à faire dans un jalon ultérieur.

## ADR-020 — La graine reste active jusqu'à la fin, retrait automatique si tout est vert — acceptée
Contexte : jusqu'à J7, le moteur arrêtait un service graine (`SeedDown`) juste après la passation de la fonction correspondante, au milieu d'`apply` ; doc02/doc05 prévoyaient en plus une commande manuelle `genesis seed retire`, jamais implémentée. Logique voulue par l'utilisateur : tous les services graine démarrent ; dès qu'un service réel est prêt et vérifié (ex. DNS), il sert à tous les services construits ensuite ; la graine reste active pour ses propres besoins ; elle est décommissionnée à la fin, sans geste manuel, si tous les tests sont verts.

Alternatives : (a) garder `SeedDown` pendant `apply`, `seed retire` ne faisant que finaliser — rejetée : pas de filet de sécurité, la graine disparaît dès la passation ; (b) `SeedDown` hors d'`apply`, déclenché par `genesis seed retire` manuel — rejetée par l'utilisateur, le geste manuel n'apporte rien si les vérifications décident.

Décision : la passation (`Handover` + repoint) ne touche plus à la graine. En fin d'`apply`, `internal/engine.retireSeed` arrête toute la graine (ordre inverse du plan) si : chaque fonction graine a un fournisseur cible, les secrets ont migré vers Vault quand une capacité `secrets` existe, et un `Verify` final (`verify.final`) de chaque module cible est vert. L'état note `seed_retired` : un `apply` ultérieur n'interroge ni ne relance plus les modules graine, ne rejoue plus `Handover`, et la clé active de chaque fonction pointe directement la cible. La commande `genesis seed retire` est supprimée.

Conséquence : l'environnement se construit et s'autonomise en un seul `apply`. Une fonction graine sans relève cible conserve la graine (l'`apply` réussit, le journal le dit) ; un `Verify` final rouge fait échouer l'`apply` avec la graine intacte. Le passage du store fichier en lecture seule (doc05) n'est pas appliqué par le code à ce jalon (dette).

## ADR-021 — Processus de développement : PR atomiques fusionnées en squash, Release Please — acceptée
Contexte : jusqu'à J8, un jalon entier tenait dans une seule branche et une seule PR, avec un commit par étape logique (la PR J8 comptait cinq sujets distincts). Ces PR sont longues à relire, et `main` ne dit pas quel commit correspond à quel sujet. L'utilisateur reprend le processus de son projet neossh : modèles d'issue et de PR, titre de PR validé en Conventional Commits, fusion en squash, Release Please, Dependabot, tests avec détecteur de concurrence.

Alternatives : (a) garder une PR par jalon avec fusion en rebase pour conserver les commits logiques — rejetée par l'utilisateur : ce sont les PR qui doivent être atomiques, pas seulement les commits ; (b) imposer une liste fermée de scopes de commit — rejetée : une liste de noms de modules obligerait à modifier `.github/` pour chaque nouveau module, et une liste limitée aux couches du cœur priverait le CHANGELOG du nom du module concerné ; le scope reste donc libre et facultatif ; (c) une seule version pour tout le dépôt — rejetée : le SDK est un module Go distinct, consommé par des modules tiers, qui a besoin de ses propres tags `sdk/vX.Y.Z` ; (d) publier les binaires avec GoReleaser dès maintenant — différée : configuration multi-binaires (cœur + un binaire par module) à concevoir.

Décision : une PR = un sujet, fusionnée en squash ; un jalon se découpe en plusieurs PR successives. Le titre de PR (validé par `semantic-prs.yml`) devient le commit sur `main`. Release Please gère deux paquets : le cœur (tag `vX.Y.Z`, version reportée dans `internal/version`, qui sert à la vérification de compatibilité `core` des modules) et le SDK (tag `sdk/vX.Y.Z`). Avant 1.0 : `feat` → patch, changement cassant → mineur, conformément à SemVer 0.x. Une release mineure franchit la borne haute des contraintes `core` : la PR qui introduit la rupture relève celle des modules du dépôt s'ils restent compatibles (constaté à la release 0.2.0, dont le changement de chemin d'import du SDK ne touchait pas le protocole gRPC ; issue de suivi sur le fondement de cette contrainte). Dependabot couvre les actions et tous les `go.mod` (une PR par dépendance, tous modules confondus). Le job CI principal lance les tests avec le détecteur de concurrence (`make test-race`, CGO pour les tests seulement). `docs/PROGRESS.md` n'est plus mis à jour à chaque PR (source de conflits permanents) mais en fin de jalon ; la dette vit dans les issues `dette-technique`. Détail dans `CONTRIBUTING.md` ; `AGENTS.md` renvoie à `CLAUDE.md` sans le dupliquer.

Conséquence : la commande `/jalon` découpe désormais le plan en PR atomiques. Aucune PR n'est fusionnée sans validation de l'utilisateur. La publication de binaires (GoReleaser) fera l'objet d'une ADR ultérieure.

## ADR-022 — Chemins de module `github.com/WhiteRoseLK/genesis`, go.mod autonomes — acceptée
Contexte : depuis J0, les modules Go s'appelaient `genesis`, `genesis/sdk` et `genesis-module-<nom>`, sans domaine, « tant que le dépôt GitHub n'existe pas ». Conséquence découverte en J3 : les `go.mod` des modules ne déclaraient aucune dépendance (même pas le SDK) et ne compilaient que grâce à `go.work`. Ni `go install`, ni un module écrit hors du dépôt, ni Dependabot ne pouvaient fonctionner. Le dépôt existe désormais et il est public.

Alternatives : (a) garder les chemins sans domaine — rejetée : le SDK est l'API publique du projet et doit être importable par un module tiers ; (b) publier tout de suite le SDK en version taguée (`sdk/v0.x`) et s'en servir dans les `require`, sans `replace` — différée : il faudrait retaguer le SDK à chaque modification du contrat pendant l'itération 1.

Décision : chemins `github.com/WhiteRoseLK/genesis` (cœur), `.../sdk`, `.../modules/<nom>` et `.../test/modules/<nom>`. Chaque `go.mod` déclare toutes ses dépendances (`go mod tidy`) et référence le SDK du dépôt par `require .../sdk v0.0.0-…` + `replace => <chemin relatif>`. `go.work` reste l'outil du développement quotidien. La CI vérifie que chaque `go.mod` est à jour et se compile hors de l'espace de travail (`make mod-check`). `genesis modules scaffold` génère un `go.mod` complet et lance `go mod tidy`.

Conséquence : l'ancienne décision de J3 (« pas de `require genesis/sdk`, pas de `go mod tidy` ») est remplacée. Les `replace` empêchent encore `go install …@version` ; ils disparaîtront quand le SDK sera publié en version taguée.
