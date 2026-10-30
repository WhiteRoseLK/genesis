# ADR-129 — fake-compute is an integration-test double, not a CLI provider

- **Status**: accepted
- **Discussion**: [#129](https://github.com/WhiteRoseLK/genesis/issues/129)

Context: `docs/07-mvp-modules.md` presented fake-compute as a way to test "the whole core and the service modules without a hypervisor", and `verify-genesis` tried to run `genesis apply` on it from the CLI. That never worked (#124, #125): `core.container/v1` reports the seed address for every detached container, which is what real VMs need to reach the seed services (#94) but gives fake-compute no address for its own containers; and its "VMs" are Alpine SSH containers without systemd, while the service playbooks target Debian. The integration tests that use fake-compute (`chrony_test.go`, `fakecompute_test.go`, `e2e_fakecompute_test.go`) pass because they fake the heavy playbooks and publish services on the loopback.

Alternatives: (a) an additive `core.container/v1` field with the container's network address — rejected: a contract change for a test double; (c) publishing each fake VM's SSH on a distinct seed port — rejected: service ports would still collide; (d) Debian fake VMs with systemd — rejected: privileged containers on the seed for a test double; removing fake-compute — rejected: the integration tests above lose their only SSH-reachable `compute.vm/v1`.

Decision: fake-compute is a `compute.vm/v1` test double for the integration tests. It is not meant to run a `genesis apply` from the CLI, and its VMs are plain SSH targets (no systemd, no Debian guarantee). `core.container/v1` keeps reporting the seed address for detached containers. A real `apply` is verified on a dedicated Proxmox test VM, on the user's explicit request.

Consequences: doc 07, the README, doc 03 and `AGENTS.md` describe fake-compute as test-only; `verify-genesis` drops its apply/destroy journey and still uses fake-compute for `validate` and `plan`. #124 and #125 are closed as intended behaviour. The module stays under `modules/`.
