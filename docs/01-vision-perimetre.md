# 01 — Vision et périmètre

## Problème
Terraform, Ansible ou Helm savent déployer des services. Ce qui n'est pas outillé, c'est le **passage de « rien » à « socle autonome »** : l'ordonnancement des dépendances croisées au démarrage.

- Il faut une heure fiable avant de créer une PKI.
- Il faut du DNS avant d'émettre des certificats nommés.
- Il faut une PKI avant de sécuriser Vault, et Vault avant d'y stocker les secrets.
- Chaque service a besoin de secrets que personne ne veut générer à la main.

Aujourd'hui ce bootstrap est fait à la main, ou via des scripts spécifiques à un client et une techno.

## Vision
> Je décris mon cluster de virtualisation et les capacités voulues dans un fichier. L'outil construit un environnement autonome, fonctionnel et reproductible, sans que j'aie à fournir d'autres secrets que ceux de l'hyperviseur.

## Principes
1. **Capacités, pas technologies** : l'utilisateur demande `dns`, pas `powerdns`. Chaque capacité a une implémentation par défaut.
2. **Zéro secret fourni** hormis les identifiants de l'hyperviseur. Tout le reste est généré.
3. **Graine jetable** : l'outil tourne sur une machine qui démarre ses propres services temporaires puis passe le relais.
4. **Modulaire** : un cœur générique, un module par produit ; ajouter un produit ou une couche ne modifie pas l'existant.
5. **Orchestrer, ne pas réinventer** : utiliser les API natives et Ansible plutôt que recoder la configuration des services.
6. **Déclaratif, idempotent, planifiable** : `plan` puis `apply`, relançable à volonté.

## Itération 1 — hypothèses simplificatrices
| Sujet | Hypothèse itération 1 | Plus tard |
|---|---|---|
| Accès Internet | La graine et les VM cibles ont Internet | Mode air-gap avec bundle d'artefacts |
| Graine | Une VM Linux (Debian/Ubuntu/RHEL-like) amd64, ou arm64 | Appliance packagée |
| Hyperviseur | Module `proxmox` uniquement, cluster existant | Modules Nutanix, vSphere, puis construction du cluster |
| Réseau | Un seul réseau plat existant, IP statiques prises dans un pool | VLAN, DHCP/PXE, multi-segments |
| Racine de confiance | Clé maîtresse en fichier local sur la graine | TPM, Shamir, HSM/YubiKey |
| Haute disponibilité | Une instance par service | HA (Vault Raft 3 nœuds, DNS secondaire…) |
| OS des VM cibles | Debian stable, image cloud + cloud-init | Autres distributions |
| Day-2 | Relance d'`apply` idempotente uniquement | Rotation, mises à jour, dérive |

## Capacités de l'itération 1 (MVP)
`time`, `dns`, `pki`, `secrets`, `bastion`.

## Non-objectifs de l'itération 1
- Couche physique (Redfish, PXE, équipements réseau). **Mais** le design ne doit pas l'empêcher : elle s'ajoutera sous forme de modules (doc 10).
- Interface web.
- Multi-utilisateurs, RBAC de l'outil lui-même.
- GitLab, supervision, sauvegarde (itération 2).

## Critère de succès global de l'itération 1
Sur un Proxmox vide, depuis une VM graine fraîche :
```
genesis init && genesis apply -f env.yaml
```
produit en moins de 30 minutes, sans intervention, un socle où : les VM ont l'heure synchronisée, résolvent la zone interne via le DNS cible, disposent de certificats émis par la PKI cible, les secrets sont dans Vault, et l'accès SSH se fait uniquement via le bastion. Une seconde exécution d'`apply` ne produit aucun changement.
