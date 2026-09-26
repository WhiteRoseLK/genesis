# Genesis

[![CI](https://github.com/WhiteRoseLK/genesis/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/WhiteRoseLK/genesis/actions/workflows/ci.yml)
[![Licence Apache 2.0](https://img.shields.io/badge/licence-Apache%202.0-blue.svg)](LICENSE)

Outil open source qui construit automatiquement un **environnement de socle autonome** (NTP, DNS, PKI, gestion des secrets, bastion, puis supervision, SCM…) sur un cluster de virtualisation existant — et à terme sur du matériel nu —, à partir d'un **unique fichier de spécification déclaratif** qui décrit des *capacités* et non des technologies.

Genesis est **modulaire** : un cœur générique qui ne connaît aucun produit, et un module par produit (plugins [go-plugin](https://github.com/hashicorp/go-plugin), gRPC). Ajouter un produit ou une couche (ex. construction de l'hyperviseur) revient à ajouter un module.

L'outil s'exécute depuis une **graine** (VM, serveur ou Raspberry Pi) qui démarre ses propres services temporaires, construit l'environnement cible, lui **transfère le relais** puis peut être retirée.

> **État** : itération 1 en cours de développement (jalons J0 à J8 faits, voir [`docs/PROGRESS.md`](docs/PROGRESS.md)). Pas encore de version publiée.

## Démarrer

Prérequis : Go (version de [`go.mod`](go.mod)), `make`, Docker ou Podman pour les tests d'intégration.

```sh
make tools        # outils de développement épinglés, dans .bin/
make build        # compile le cœur et tous les modules
make test         # tests unitaires (sans réseau ni conteneur)
make test-docker  # tests d'intégration contre de vrais conteneurs
make lint
```

```sh
go run ./cmd/genesis --help
go run ./cmd/genesis validate -f spec.yaml
```

Le dépôt est un monorepo : cœur (`cmd/`, `internal/`), SDK des modules (`sdk/`, module Go distinct), modules (`modules/<nom>/`), chacun avec son propre `go.mod`. Voir [`CONTRIBUTING.md`](CONTRIBUTING.md) pour le processus de contribution et [`SECURITY.md`](SECURITY.md) pour signaler une vulnérabilité.

## Documents de conception

| # | Document | Contenu |
|---|----------|---------|
| 01 | [Vision et périmètre](docs/01-vision-perimetre.md) | Problème, objectifs, non-objectifs, hypothèses de l'itération 1 |
| 02 | [Architecture modulaire](docs/02-architecture.md) | Cœur, modules, fonctions, broker, organisation du dépôt |
| 03 | [Contrat de module](docs/03-contrat-module.md) | Manifest, protocole gRPC, fonctions, règles |
| 04 | [Format de la spec](docs/04-spec.md) | Schéma YAML, règles de validation, exemple complet |
| 05 | [Cycle de bootstrap](docs/05-cycle-bootstrap.md) | Séquence graine → cible → passation → retrait |
| 06 | [Secrets et état](docs/06-secrets-etat.md) | Génération, stockage, migration vers Vault, fichier d'état |
| 07 | [Modules de l'itération 1](docs/07-modules-mvp.md) | proxmox, base-os, chrony, coredns, powerdns, step-ca, vault, teleport |
| 08 | [Jalons et critères d'acceptation](docs/08-jalons.md) | Plan de développement incrémental |
| 09 | [Décisions (ADR)](docs/09-decisions.md) | Choix structurants et dette assumée |
| 10 | [Ajouter un module](docs/10-ajouter-un-module.md) | Procédure, exemples Bind, GitLab, construction de l'hyperviseur |

Avancement : [`docs/PROGRESS.md`](docs/PROGRESS.md) · historique des jalons : [`docs/journal.md`](docs/journal.md) · dette technique : [issues `dette-technique`](https://github.com/WhiteRoseLK/genesis/issues?q=is%3Aissue+is%3Aopen+label%3Adette-technique).

Développement assisté par Claude Code : voir [`CLAUDE.md`](CLAUDE.md), les règles dans `.claude/rules/` et la commande `/jalon`.

## Licence

[Apache 2.0](LICENSE) (ADR-009).
