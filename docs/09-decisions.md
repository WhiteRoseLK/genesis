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
