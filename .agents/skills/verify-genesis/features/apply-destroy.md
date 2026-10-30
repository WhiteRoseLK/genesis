# Apply and destroy

A user runs `apply` to build the environment (seed services in containers, target VMs, configuration, verification, handover, seed retirement), then `destroy` to remove it. With fake-compute, "VMs" are SSH containers on the local Docker daemon.

**Status: failing.** `apply` stops at `configure` because the target VMs are unreachable (#124), and `destroy` leaves the VM containers behind (#125). Run this journey to verify a fix for either issue, and report it as failing otherwise.

## Sub-features

- `apply-seed` starts the seed services (`genesis-coredns`, `genesis-step-ca`).
- `apply-target` provisions, configures and verifies each target module.
- `apply-idempotent` a second `apply` changes nothing.
- `destroy` removes every resource the apply created.

## How to get to it (user POV)

- `genesis apply -f <spec> [--auto-approve]`
- `genesis destroy -f <spec> [--auto-approve]`

## Driving it with g.sh

Preconditions:

- Launched run, doctor passes (in particular: no foreign `genesis-*` container), no other run or `make test-docker` using Docker.

- **Apply.** Run `genesis apply -f spec.yaml --auto-approve` (a few minutes; images are pulled on first use). Expected: one `INFO step module=<m> step=<step> outcome=ok` line per step and exit `0`. Today: exit `1` after `module=chrony step=provision outcome=ok`, with Ansible `UNREACHABLE … connect to host 10.10.0.5x port 22` (#124).
- **Side effects.** Run `docker ps --filter name=genesis- --format '{{.Names}} {{.Status}}'`: `genesis-coredns` and one `genesis-vm-<name>` per target VM (`chrony01`, `powerdns01`, `vault01`, `teleport01`, `genesis-verifier`). Save the output next to the transcripts: `docker ps … > "$GENESIS_VERIFY_ROOT/evidence/<run-id>/docker-after-apply.txt"`.
- **State.** Run `genesis status -f spec.yaml`: each module listed with its state.
- **Idempotence.** Run `genesis apply -f spec.yaml --auto-approve` again: every step `compliant, no action`.
- **Destroy.** Run `genesis destroy -f spec.yaml --auto-approve`: `destroy complete.` Second view: `docker ps -a --filter name=genesis-` is empty. Today the five `genesis-vm-*` containers remain (#125); `cleanup.sh` records them in `leftover-containers-after-destroy.txt` and removes them.

## Gotchas

- Container names are fixed: never run this journey while another run or `make test-docker` is using Docker.
- On a Mac, Docker Desktop container addresses are not routable from the host; once #124 is fixed, check that the fix works there too and say which platform the proof ran on.
- Always finish with `cleanup.sh`, even after a failed apply: it is the only step that removes the containers this run created.
