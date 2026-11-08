# 04 — Spec format

The spec describes **capabilities and their parameters**. Choosing a module is optional: without `module`, the capability's default module is used. Each capability's configuration section is validated by the `config_schema` of the chosen module.

## Full example (iteration 1)

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
  compute:                       # the virtualisation platform is a capability like any other
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
    module: vault                # same module as pki → same instance
  bastion:
    module: teleport
    config:
      allowed_users: [admin]

placement:                       # optional
  infra01: [time, dns]
  vault01: [pki, secrets]
  bastion01: [bastion]
```

## Equivalent minimal version (all defaults)

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
`bastion` automatically pulls in `pki`, `dns`, `time`, `os.base` through the `requires` → the plan shows the added modules.

## Rules
- **No literal secret**: `*_ref` fields in `env://`, `file://` or `vault://` only.
- `module` missing → the module marked `defaults.<capability>: true`; if several are installed, an error asks for a choice.
- A module added automatically through `requires` follows the same rule: when several modules provide a required function, the one marked default for a capability it declares and another candidate also declares is chosen; otherwise the error asks for a choice (ADR-135).
- `genesis plan` shows how each capability's module was chosen in its SOURCE column: `declared in spec (pki)` when the spec names the module, `declared in spec (pki, default)` for a default (ADR-135), `declared in spec (pki, only provider)` when a single installed module provides the capability. A module serving several capabilities with different origins marks each non-explicit one, e.g. `(pki, secrets [default])`.
- Default modules (ADR-135): `time` → `chrony`, `dns` → `powerdns`, `pki` → `vault`, `secrets` → `vault`, `bastion` → `teleport`. `coredns`, `bind` and `step-ca` declare their capability without being the default: select them with `module:`.
- Two capabilities pointing to the same module share its instance.
- Two-stage validation: overall structure by the core, then `config` by the module (errors with the full YAML path).
- Large enough IP pool, no port conflict between co-hosted modules (ports declared in the manifests).
- `apiVersion` is mandatory; conversions live in `internal/spec/convert`.

## Future extension: physical layer
No change to the format is needed, only new capabilities:

```yaml
capabilities:
  baremetal:
    module: redfish
    config: { servers: [ { bmc: 10.0.0.11, credentials_ref: env://BMC1 } ] }
  compute:
    module: proxmox-install      # installs the cluster, then provides compute.vm/v1
    config: { nodes: 3 }
```
