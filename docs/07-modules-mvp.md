# 07 — Modules de l'itération 1

Chaque module vit dans `modules/<nom>/` avec son `module.yaml`, son code, ses rôles Ansible dans `assets/ansible/` et ses tests.

## `fake-compute` (couche platform, tests)
- **Fournit** : `compute.vm/v1`.
- « VM » = conteneurs systemd sur la graine, joignables en SSH. Permet de tester tout le cœur et les modules de service sans hyperviseur.

## `proxmox` (couche platform)
- **Fournit** : `compute.vm/v1`. **Requiert** : `core.secrets/v1`.
- `Validate` : accès API, droits du token, nœud, stockage et bridge existants.
- `EnsureImage` : télécharge l'image cloud Debian et crée un template (une fois).
- `EnsureVM` : clone lié du template, cloud-init (IP, clé SSH de service, utilisateur `genesis`), démarrage, attente de SSH. Clé d'idempotence : nom de VM + tag `genesis-env=<nom>`.
- `Now` : heure du nœud (contrôle d'horloge de la graine).
- `extra` accepté : `cpu_type`, `numa`, `tags`, `ha_group`…

## `base-os` (couche foundation)
- **Fournit** : `os.base/v1`. **Requiert** : `core.ansible/v1`.
- Durcissement SSH, mises à jour, nftables `drop` + ouvertures déclarées, installation de la CA, résolveur et NTP (appelés en `Repoint`).
- Appelé par chaque module qui crée une VM, juste après `EnsureVM`.

## `chrony` (foundation)
- **Fournit** : `time.ntp/v1` (target). **Requiert** : `compute.vm`, `os.base`.
- **Verify** : depuis une autre VM, source sélectionnée, écart < 100 ms.
- Déclenche `Repoint` NTP sur toutes les VM.

## `coredns` (foundation, phase graine)
- **Fournit** : `dns.zone/v1`, `dns.resolver/v1` en phase `seed`. **Requiert** : `core.container`.
- Zone régénérée à chaque `UpsertRecord`.

## `powerdns` (foundation)
- **Fournit** : `dns.zone/v1`, `dns.resolver/v1` (target). **Requiert** : `compute.vm`, `os.base`, `time.ntp`, `core.secrets`, `dns.zone@seed`.
- Authoritative (SQLite, API) + Recursor.
- **Handover** : relit la zone via `dns.zone@seed.ListRecords`, recrée tout, compare.
- **Verify** : résolution directe/inverse depuis une VM tierce + nom externe.

## `step-ca` (foundation, phase graine)
- **Fournit** : `pki.issuer/v1` en phase `seed`. **Requiert** : `core.container`, `core.secrets`.
- Génère la CA racine (stockée en `recovery`) et un intermédiaire « seed » de 30 jours.

## `vault` (foundation)
- **Fournit** : `pki.issuer/v1`, `secrets.kv/v1` (target). **Requiert** : voir manifest du doc 03.
- Raft mono-nœud, TLS via `pki.issuer@seed`, init 5/3, descellement par le module, `pki_int` signé par la racine, KV v2, AppRole `genesis`.
- **Handover** : bascule `pki.issuer`, réémission de son propre certificat, migration des secrets du backend fichiers (orchestrée par le cœur via `core.secrets`), révocation du token root.
- **Verify** : émission et validation de chaîne depuis une VM tierce ; lecture KV via AppRole.

## `openssh-bastion` (services)
- **Fournit** : `access.ssh/v1`. **Requiert** : `compute.vm`, `os.base`, `pki.issuer` (SignSSH), `dns.zone`.
- CA SSH utilisateur de Vault déployée en `TrustedUserCAKeys` ; SSH des VM limité à l'IP du bastion.
- **Repoint** : le cœur bascule ses propres runners SSH en `ProxyJump` par le bastion.
- **Verify** : connexion via bastion OK, directe refusée.
