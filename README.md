# Genesis — dossier de conception

> Licence : Apache 2.0 (voir ADR-009).

Outil open source qui construit automatiquement un **environnement de socle autonome** (NTP, DNS, PKI, gestion des secrets, bastion, puis supervision, SCM…) sur un cluster de virtualisation existant — et à terme sur du matériel nu —, à partir d'un **unique fichier de spécification déclaratif** qui décrit des *capacités* et non des technologies.

Genesis est **modulaire** : un cœur générique qui ne connaît aucun produit, et un module par produit. Ajouter un produit ou une couche (ex. construction de l'hyperviseur) revient à ajouter un module.

L'outil s'exécute depuis une **graine** (VM, serveur ou Raspberry Pi) qui démarre ses propres services temporaires, construit l'environnement cible, lui **transfère le relais** puis peut être retirée.

## Documents

| # | Document | Contenu |
|---|----------|---------|
| 01 | [Vision et périmètre](docs/01-vision-perimetre.md) | Problème, objectifs, non-objectifs, hypothèses de l'itération 1 |
| 02 | [Architecture modulaire](docs/02-architecture.md) | Cœur, modules, fonctions, broker, organisation du dépôt |
| 03 | [Contrat de module](docs/03-contrat-module.md) | Manifest, protocole gRPC, fonctions, règles |
| 04 | [Format de la spec](docs/04-spec.md) | Schéma YAML, règles de validation, exemple complet |
| 05 | [Cycle de bootstrap](docs/05-cycle-bootstrap.md) | Séquence graine → cible → passation → retrait |
| 06 | [Secrets et état](docs/06-secrets-etat.md) | Génération, stockage, migration vers Vault, fichier d'état |
| 07 | [Modules de l'itération 1](docs/07-modules-mvp.md) | proxmox, base-os, chrony, coredns, powerdns, step-ca, vault, openssh-bastion |
| 08 | [Jalons et critères d'acceptation](docs/08-jalons.md) | Plan de développement incrémental |
| 09 | [Décisions (ADR)](docs/09-decisions.md) | Choix structurants et dette assumée |
| 10 | [Ajouter un module](docs/10-ajouter-un-module.md) | Procédure, exemples Bind, GitLab, construction de l'hyperviseur |

Développement assisté par Claude Code : voir [CLAUDE.md](CLAUDE.md), les règles dans `.claude/rules/` et la commande `/jalon J0`.
