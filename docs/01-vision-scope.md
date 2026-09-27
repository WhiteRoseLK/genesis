# 01 — Vision and scope

## Problem
Terraform, Ansible or Helm can deploy services. What has no tooling is the **journey from "nothing" to "self-sufficient foundation"**: ordering the cross-dependencies at startup.

- You need a reliable clock before creating a PKI.
- You need DNS before issuing named certificates.
- You need a PKI before securing Vault, and Vault before storing secrets in it.
- Every service needs secrets that nobody wants to generate by hand.

Today this bootstrap is done by hand, or with scripts specific to one customer and one technology.

## Vision
> I describe my virtualisation cluster and the capabilities I want in a file. The tool builds a self-sufficient, working and reproducible environment, without me having to provide any secret other than the hypervisor's.

## Principles
1. **Capabilities, not technologies**: the user asks for `dns`, not `powerdns`. Each capability has a default implementation.
2. **Zero secrets provided** apart from the hypervisor credentials. Everything else is generated.
3. **Disposable seed**: the tool runs on a machine that starts its own temporary services, then hands over.
4. **Modular**: a generic core, one module per product; adding a product or a layer does not change what exists.
5. **Orchestrate, don't reinvent**: use native APIs and Ansible rather than re-coding service configuration.
6. **Declarative, idempotent, plannable**: `plan` then `apply`, re-runnable at will.

## Iteration 1 — simplifying assumptions
| Topic | Iteration 1 assumption | Later |
|---|---|---|
| Internet access | The seed and target VMs have Internet access | Air-gapped mode with an artefact bundle |
| Seed | A Linux VM (Debian/Ubuntu/RHEL-like), amd64 or arm64 | Packaged appliance |
| Hypervisor | `proxmox` module only, existing cluster | Nutanix and vSphere modules, then building the cluster |
| Network | A single existing flat network, static IPs taken from a pool | VLANs, DHCP/PXE, multiple segments |
| Root of trust | Master key in a local file on the seed | TPM, Shamir, HSM/YubiKey |
| High availability | One instance per service | HA (3-node Vault Raft, secondary DNS…) |
| Target VM OS | Debian stable, cloud image + cloud-init | Other distributions |
| Day 2 | Idempotent re-run of `apply` only | Rotation, upgrades, drift |

## Iteration 1 capabilities (MVP)
`time`, `dns`, `pki`, `secrets`, `bastion`.

## Iteration 1 non-goals
- Physical layer (Redfish, PXE, network equipment). **But** the design must not prevent it: it will be added as modules (doc 10).
- Web interface.
- Multiple users, RBAC for the tool itself.
- GitLab, monitoring, backup (iteration 2).

## Overall success criterion for iteration 1
On an empty Proxmox, from a fresh seed VM:
```
genesis init && genesis apply -f env.yaml
```
produces, in under 30 minutes and without intervention, a foundation where: the VMs have a synchronised clock, resolve the internal zone through the target DNS, hold certificates issued by the target PKI, the secrets are in Vault, and SSH access goes only through the bastion. A second run of `apply` produces no change.
