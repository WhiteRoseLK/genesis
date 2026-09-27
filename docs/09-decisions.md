# 09 — Decision record (ADR)

Each structural decision is a file in [`docs/adr/`](adr/): context → alternatives → decision → consequences (ADR-049).

**New decision**:
1. Open an issue from the **"Decision"** template (label `decision`): context, options, recommendation.
2. The user decides in a comment on the issue.
3. The PR that applies the decision copies [`adr/_template.md`](adr/_template.md) to `adr/NNNN-title.md` (NNNN = the issue number), adds its row below and closes the issue (`Closes #NNNN`).

A superseded decision is not deleted: its status becomes "superseded by ADR-NNN". ADRs 001 to 022 predate this process and keep their historical numbers.

| ADR | Decision | Status |
|---|---|---|
| [ADR-001](adr/0001-go-single-binary.md) | Go, single binary | accepted |
| [ADR-002](adr/0002-orchestrate-ansible-instead-of-recoding-configuration.md) | Orchestrate Ansible instead of re-coding configuration | accepted |
| [ADR-003](adr/0003-no-terraform-opentofu-in-iteration-1.md) | No Terraform/OpenTofu in iteration 1 | accepted |
| [ADR-004](adr/0004-proxmox-as-first-compute-module.md) | Proxmox as the first compute module | accepted |
| [ADR-005](adr/0005-capabilities-described-by-seed-target-functions.md) | Capabilities described by functions provided by the seed or the target | accepted |
| [ADR-006](adr/0006-age-for-the-local-secret-backend.md) | age for the local secret backend | accepted |
| [ADR-007](adr/0007-master-key-in-a-local-file.md) | Master key in a local file | accepted (debt) |
| [ADR-008](adr/0008-connected-profile-only-in-iteration-1.md) | `connected` profile only in iteration 1 | accepted (debt) |
| [ADR-009](adr/0009-apache-2-0-license.md) | Apache 2.0 license | accepted |
| [ADR-010](adr/0010-project-name-genesis.md) | Project name: Genesis | accepted |
| [ADR-011](adr/0011-core-plus-modules-architecture-no-monolith.md) | Core + modules architecture, no monolith | accepted |
| [ADR-012](adr/0012-modules-as-separate-processes-via-hashicorp-go-plugin-grpc.md) | Modules as separate processes via HashiCorp go-plugin (gRPC) | accepted |
| [ADR-013](adr/0013-monorepo-separate-artefacts.md) | Monorepo, separate artefacts | accepted |
| [ADR-014](adr/0014-ports-declared-in-manifests.md) | Ports declared in the manifests | accepted |
| [ADR-015](adr/0015-santhosh-tekuri-jsonschema-v5-for-json-schema-validation.md) | `santhosh-tekuri/jsonschema/v5` for JSON Schema validation | accepted |
| [ADR-016](adr/0016-core-ansible-v1-open-to-any-target-module.md) | `core.ansible/v1` open to any target module, not reserved for `base-os` | accepted |
| [ADR-017](adr/0017-fleet-modules-a-function-can-have-several-active-providers.md) | "Fleet" modules: a function can have several providers active at the same time | accepted |
| [ADR-018](adr/0018-core-ansible-v1-via-openssh-certificate-not-tsh.md) | `core.ansible/v1` via an OpenSSH certificate, not via `tsh` | accepted |
| [ADR-019](adr/0019-ssh-repoint-to-teleport-agent-deferred-beyond-m8.md) | SSH repoint to the Teleport agent deferred beyond M8 | accepted |
| [ADR-020](adr/0020-seed-stays-active-until-the-end-automatic-retirement.md) | The seed stays active until the end, automatic retirement if everything is green | accepted |
| [ADR-021](adr/0021-development-process-atomic-squash-merged-prs.md) | Development process: atomic PRs merged with squash, Release Please | accepted; milestone and debt tracking completed by ADR-049 |
| [ADR-022](adr/0022-module-paths-github-com-whiteroselk-genesis-self-contained-go-mod.md) | Module paths `github.com/WhiteRoseLK/genesis`, self-contained go.mod files | accepted |
| [ADR-049](adr/0049-milestones-on-github-adrs-numbered-by-issue.md) | Milestones tracked on GitHub, ADRs as files numbered by their issue | accepted |
| [ADR-054](adr/0054-english-as-the-project-language.md) | English as the project language | accepted |
