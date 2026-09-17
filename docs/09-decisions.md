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
