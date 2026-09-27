# 10 — Adding a module

Goal: prove that the architecture is extensible. **Adding a product changes neither the core, nor the spec, nor the other modules.**

## Steps
1. `genesis modules scaffold <name> --provides dns.zone/v1,dns.resolver/v1`
   Generates `modules/<name>/`: `module.yaml`, `main.go` with `sdk.Serve`, stubs for each RPC, `schema.json`, conformance test.
2. Fill in the manifest: `provides`, `requires`, `capabilities`, secrets, resources, ports.
3. Implement the lifecycle and the services of the provided functions.
4. Write the `config_schema`.
5. Pass the SDK **conformance suite**: `go test ./... -run Conformance`. It checks idempotence, that `Handover` can be replayed, that no secret appears in the outputs, and the semantics of each provided function (e.g. an `UpsertRecord` must be visible through `ListRecords`, then resolvable).
6. `genesis modules install ./modules/<name>`, then reference `module: <name>` in a spec.

## Example 1 — Replacing PowerDNS with Bind
- A `bind` module providing `dns.zone/v1` and `dns.resolver/v1`, `requires`: `compute.vm`, `os.base`, `dns.zone@seed`.
- Spec: `dns: { module: bind }`.
- No change in `vault`, `teleport` or the core: they call `dns.zone/v1`, not PowerDNS.

## Example 2 — Adding GitLab (iteration 2)
- A `gitlab` module, `scm` capability, provides `scm.git/v1`.
- `requires`: `compute.vm`, `os.base`, `dns.zone`, `pki.issuer`, `secrets.kv`, `access.ssh`.
- The planner automatically places it after the bastion.

## Example 3 — Building the hypervisor before everything else (later)
No notion of order to code: it is enough for the `compute.vm/v1` function to be provided by a module that itself depends on the physical layer.

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

- `pxe-seed`: a seed-phase module, provides `dhcp.pxe/v1` (DHCP/TFTP/HTTP in containers on the seed).
- `redfish`: provides `baremetal.server/v1` (inventory, BIOS, PXE boot, OS installation).
- `proxmox-install`: requires `baremetal.server`, provides `platform.cluster/v1` (Proxmox installation, cluster creation, API token generated in `core.secrets`).
- `proxmox`: its `requires` gains an **optional** `platform.cluster/v1` (`optional: true`): absent → existing cluster (iteration 1); present → it consumes the generated endpoint and credentials.

The rest of the environment is strictly identical.

## Optional dependencies
The manifest accepts:
```yaml
requires:
  target:
    - function: platform.cluster/v1
      optional: true
```
If a provider is present in the resolution, the dependency becomes an edge of the DAG; otherwise it is ignored. This is the mechanism that allows new layers to be stacked beneath existing modules.
