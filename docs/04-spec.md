# 04 — Format de la spécification

La spec décrit **des capacités et leurs paramètres**. Le choix d'un module est facultatif : sans `module`, le module par défaut de la capacité est utilisé. La section de configuration de chaque capacité est validée par le `config_schema` du module choisi.

## Exemple complet (itération 1)

```yaml
apiVersion: genesis/v1alpha1
kind: Environment
metadata:
  name: lab

network:
  cidr: 10.10.0.0/24
  gateway: 10.10.0.1
  pool: 10.10.0.50-10.10.0.99
  domain: lab.internal
  upstream_dns: [1.1.1.1, 9.9.9.9]
  upstream_ntp: [0.fr.pool.ntp.org, 1.fr.pool.ntp.org]

seed:
  address: 10.10.0.10
  state_dir: /var/lib/genesis
  container_runtime: auto

profile: connected

sizes:
  small:  { cpu: 1, memory_mb: 1024, disk_gb: 10 }
  medium: { cpu: 2, memory_mb: 4096, disk_gb: 30 }

capabilities:
  compute:                       # la plateforme de virtualisation est une capacité comme les autres
    module: proxmox
    config:
      endpoint: https://pve01.home.arpa:8006
      node: pve01
      storage: local-lvm
      bridge: vmbr0
      image: debian-13
      credentials:
        token_id_ref: env://PVE_TOKEN_ID
        token_secret_ref: env://PVE_TOKEN_SECRET
  time:
    module: chrony
  dns:
    module: powerdns
  pki:
    module: vault
    config:
      root_ca: { common_name: Lab Root CA, validity: 10y }
      default_cert_ttl: 90d
  secrets:
    module: vault                # même module que pki → même instance
  bastion:
    module: openssh-bastion
    config:
      allowed_users: [admin]

placement:                       # facultatif
  infra01: [time, dns]
  vault01: [pki, secrets]
  bastion01: [bastion]
```

## Version minimale équivalente (tout par défaut)

```yaml
apiVersion: genesis/v1alpha1
kind: Environment
metadata: { name: lab }
network: { cidr: 10.10.0.0/24, gateway: 10.10.0.1, pool: 10.10.0.50-10.10.0.99, domain: lab.internal }
seed: { address: 10.10.0.10 }
capabilities:
  compute:
    module: proxmox
    config: { endpoint: https://pve01.home.arpa:8006, node: pve01, credentials: { token_id_ref: env://PVE_TOKEN_ID, token_secret_ref: env://PVE_TOKEN_SECRET } }
  bastion: {}
```
`bastion` entraîne automatiquement `pki`, `dns`, `time`, `os.base` via les `requires` → le plan affiche les modules ajoutés.

## Règles
- **Aucun secret littéral** : champs `*_ref` en `env://`, `file://` ou `vault://` uniquement.
- `module` absent → module marqué `defaults.<capacité>: true` ; s'il y en a plusieurs installés, erreur demandant un choix.
- Deux capacités pointant sur le même module partagent son instance.
- Validation en deux temps : structure générale par le cœur, puis `config` par le module (erreurs avec chemin YAML complet).
- Pool IP suffisant, pas de conflit de ports entre modules co-hébergés (ports déclarés dans les manifests).
- `apiVersion` obligatoire ; conversions dans `internal/spec/convert`.

## Extension future : couche physique
Aucune modification du format n'est nécessaire, seulement de nouvelles capacités :

```yaml
capabilities:
  baremetal:
    module: redfish
    config: { servers: [ { bmc: 10.0.0.11, credentials_ref: env://BMC1 } ] }
  compute:
    module: proxmox-install      # installe le cluster puis fournit compute.vm/v1
    config: { nodes: 3 }
```
