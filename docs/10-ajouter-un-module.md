# 10 — Ajouter un module

Objectif : prouver que l'architecture est extensible. **Ajouter un produit ne modifie ni le cœur, ni la spec, ni les autres modules.**

## Étapes
1. `genesis modules scaffold <nom> --provides dns.zone/v1,dns.resolver/v1`
   Génère `modules/<nom>/` : `module.yaml`, `main.go` avec `sdk.Serve`, stubs de chaque RPC, `schema.json`, test de conformité.
2. Remplir le manifest : `provides`, `requires`, `capabilities`, secrets, ressources, ports.
3. Implémenter le cycle de vie et les services des fonctions fournies.
4. Écrire le `config_schema`.
5. Passer la **suite de conformité** du SDK : `go test ./... -run Conformance`. Elle vérifie idempotence, rejouabilité de `Handover`, absence de secrets dans les sorties, et le respect sémantique de chaque fonction fournie (ex. un `UpsertRecord` doit être visible par `ListRecords` puis résolvable).
6. `genesis modules install ./modules/<nom>` puis référencer `module: <nom>` dans une spec.

## Exemple 1 — Remplacer PowerDNS par Bind
- Module `bind` fournissant `dns.zone/v1` et `dns.resolver/v1`, `requires` : `compute.vm`, `os.base`, `dns.zone@seed`.
- Spec : `dns: { module: bind }`.
- Aucun changement dans `vault`, `openssh-bastion` ou le cœur : ils appellent `dns.zone/v1`, pas PowerDNS.

## Exemple 2 — Ajouter GitLab (itération 2)
- Module `gitlab`, capacité `scm`, fournit `scm.git/v1`.
- `requires` : `compute.vm`, `os.base`, `dns.zone`, `pki.issuer`, `secrets.kv`, `access.ssh`.
- Le planificateur le place automatiquement après le bastion.

## Exemple 3 — Construire l'hyperviseur avant le reste (plus tard)
Aucune notion d'ordre à coder : il suffit que la fonction `compute.vm/v1` soit fournie par un module qui dépend lui-même de la couche physique.

```mermaid
flowchart LR
  subgraph physical
    DHCP[pxe-seed<br/>dhcp.pxe@seed]
    RF[redfish<br/>baremetal.server]
  end
  subgraph platform
    PI[proxmox-install<br/>platform.cluster]
    PX[proxmox<br/>compute.vm]
  end
  subgraph foundation
    DNS[powerdns] --> V[vault]
  end
  DHCP --> RF --> PI --> PX --> DNS
```

- `pxe-seed` : module de phase graine, fournit `dhcp.pxe/v1` (DHCP/TFTP/HTTP en conteneurs sur la graine).
- `redfish` : fournit `baremetal.server/v1` (inventaire, BIOS, démarrage PXE, installation OS).
- `proxmox-install` : requiert `baremetal.server`, fournit `platform.cluster/v1` (installation Proxmox, création du cluster, token API généré dans `core.secrets`).
- `proxmox` : son `requires` gagne `platform.cluster/v1` **optionnel** (`optional: true`) : absent → cluster existant (itération 1) ; présent → il consomme l'endpoint et les identifiants générés.

Le reste de l'environnement est strictement identique.

## Dépendances optionnelles
Le manifest accepte :
```yaml
requires:
  target:
    - function: platform.cluster/v1
      optional: true
```
Si un fournisseur est présent dans la résolution, la dépendance devient une arête du DAG ; sinon elle est ignorée. C'est le mécanisme qui permet d'empiler de nouvelles couches sous des modules existants.
