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

## `teleport` (services, module « de parc »)
- **Fournit** : `access.ssh/v1`, `fleet.agent/v1` (ADR-017 : fonction à fournisseurs multiples — plusieurs modules « de parc » peuvent coexister, tous appelés). **Requiert** : `compute.vm`, `os.base`, `core.ansible`, `core.secrets`, `time.ntp`, `dns.resolver`, `pki.issuer` (certificat TLS du service Auth/Proxy, résolu vers `vault`).
- Teleport (Auth + Proxy) déployé sur sa propre VM, avec sa propre CA interne pour les certificats SSH utilisateur et hôte (pas de délégation à `pki.issuer/v1` pour la signature SSH — mécanisme propre à Teleport, ADR-018). Agent Teleport installé sur **chaque** VM du parc : tout module qui provisionne une VM (`chrony`, `powerdns`, `vault`…) déclare `fleet.agent/v1` dans ses `requires` et l'appelle depuis `Configure`, comme il appelle déjà `os.base/v1` — c'est ce qui rend Teleport actif sur tout le parc dès sa présence dans la spec, sans qu'aucun module existant ne connaisse Teleport spécifiquement (doc02).
- **Repoint différé (dette, hors périmètre J8)** : `fleet.agent/v1.Install` installe et enrôle l'agent Teleport (`ssh_service`, port 3022) sur la VM cible mais laisse le sshd natif actif — `core.ansible/v1` continue d'utiliser l'accès SSH direct par clé. Faire basculer les runners `core.ansible/v1` du cœur vers l'agent Teleport (et alors seulement désactiver le sshd natif) est un chantier cœur à part entière, tracé par une issue dédiée plutôt que traité dans ce jalon.
- **Verify** : connexion SSH réelle à travers l'agent Teleport OK (certificat utilisateur signé par `tctl auth sign`, depuis une VM tierce). Ne prouve pas le refus de l'accès direct (cf. Repoint différé ci-dessus).
