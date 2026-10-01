# 07 — Iteration 1 modules

Each module lives in `modules/<name>/` with its `module.yaml`, its code, its Ansible roles in `assets/ansible/` and its tests.

## `fake-compute` (platform layer, tests)
- **Provides**: `compute.vm/v1`.
- "VM" = systemd containers on the seed, reachable over SSH. Allows testing the whole core and the service modules without a hypervisor.

## `proxmox` (platform layer)
- **Provides**: `compute.vm/v1`. **Requires**: `core.secrets/v1`.
- `Validate`: API access, token permissions, existing node, storage and bridge.
- `EnsureImage`: downloads the Debian cloud image and creates a template (once).
- `EnsureVM`: linked clone of the template, cloud-init (IP, service SSH key, `genesis` user), start, wait for SSH. Idempotence key: VM name + tag `genesis-env=<name>`.
- `Now`: the node's clock (seed clock check).
- Accepted `extra`: `cpu_type`, `numa`, `tags`, `ha_group`…

## `base-os` (foundation layer)
- **Provides**: `os.base/v1`. **Requires**: `core.ansible/v1`.
- SSH hardening, updates, nftables `drop` + declared openings, CA installation, resolver and NTP (called during `Repoint`).
- Called by every module that creates a VM, right after `EnsureVM`.

## `chrony` (foundation)
- **Provides**: `time.ntp/v1` (target). **Requires**: `compute.vm`, `os.base`.
- **Verify**: from another VM, source selected, offset < 100 ms.
- Triggers an NTP `Repoint` on every VM.

## `coredns` (foundation, seed phase)
- **Provides**: `dns.zone/v1`, `dns.resolver/v1` in the `seed` phase. **Requires**: `core.container`.
- Zone regenerated on every `UpsertRecord`.

## `powerdns` (foundation)
- **Provides**: `dns.zone/v1`, `dns.resolver/v1` (target). **Requires**: `compute.vm`, `os.base`, `time.ntp`, `core.secrets`, `dns.zone@seed`.
- Authoritative (SQLite, API) + Recursor.
- **Handover**: reads the zone back through `dns.zone@seed.ListRecords`, recreates everything, compares.
- **Verify**: forward/reverse resolution from a third-party VM + an external name.

## `step-ca` (foundation, seed phase)
- **Provides**: `pki.issuer/v1` in the `seed` phase. **Requires**: `core.container`, `core.secrets`.
- Generates the root CA (stored as `recovery`) and a 30-day "seed" intermediate.

## `vault` (foundation)
- **Provides**: `pki.issuer/v1`, `secrets.kv/v1` (target). **Requires**: see the manifest in doc 03.
- Single-node Raft, TLS via `pki.issuer@seed`, init 5/3, unsealing by the module, `pki_int` signed by the root, KV v2, `genesis` AppRole.
- **Handover**: switches `pki.issuer`, reissues its own certificate, migrates the secrets from the file backend (orchestrated by the core through `core.secrets`), revokes the root token.
- **Verify**: issuance and chain validation from a third-party VM; KV read through the AppRole.

## `teleport` (services, "fleet" module)
- **Provides**: `access.ssh/v1`, `fleet.agent/v1` (ADR-017: multi-provider function — several "fleet" modules can coexist, all of them called). **Requires**: `compute.vm`, `os.base`, `core.ansible`, `core.secrets`, `time.ntp`, `dns.resolver`, `pki.issuer` (TLS certificate of the Auth/Proxy service, resolved to `vault`).
- Teleport (Auth + Proxy) deployed on its own VM, with its own internal CA for user and host SSH certificates (no delegation to `pki.issuer/v1` for SSH signing — a mechanism specific to Teleport, ADR-018). A Teleport agent is installed on **every** VM in the fleet: every module that provisions a VM (`chrony`, `powerdns`, `vault`…) declares `fleet.agent/v1` in its `requires` and calls it from `Configure`, just as it already calls `os.base/v1` — this is what makes Teleport active across the whole fleet as soon as it is in the spec, without any existing module knowing about Teleport specifically (doc 02).
- **SSH Repoint**: `fleet.agent/v1.Install` installs and enrols the Teleport agent (`ssh_service`, port 3022) on the target VM and disables the native sshd service once enrolment is confirmed. Callers switch to the Teleport agent on port 3022 with an OpenSSH user certificate signed by `access.ssh/v1.SignUserKey` (ADR-018).
- **Verify**: a real SSH connection through the Teleport agent succeeds (user certificate signed by `tctl auth sign`, from a third-party VM), and direct SSH access to the native sshd on port 22 is refused.
