---
name: verify-genesis
description: "Drive the real genesis CLI end to end like a user (modules install/verify, validate, plan, init, status, secrets, clean) in a disposable run, and keep transcripts as proof. Use before declaring a Genesis change done, to reproduce a bug from the CLI, or when asked to verify Genesis behaviour. Never runs apply and never touches Proxmox."
---

# verify-genesis

Genesis is a CLI (`cmd/genesis`) that turns a YAML spec into infrastructure through plugin modules. A user installs modules, writes a spec, then runs `init`, `validate`, `plan`, `apply`, `status`, `secrets`, `destroy`, `clean`. This skill drives that real binary, built from the current checkout, inside a disposable **run**: its own `HOME` (module search path), its own `state_dir`, `NO_COLOR=1`, and the `fake-compute` module instead of a hypervisor.

This skill never runs `apply` or `destroy`. fake-compute is an integration-test double and cannot run a CLI `apply` (ADR-129); it is only there so `validate` and `plan` resolve a compute provider. A real `apply` is verified on a dedicated Proxmox test VM, and only on the user's explicit request (`AGENTS.md`). `make e2e` is out of scope here.

All paths below are relative to this skill's directory. Helpers must be run from inside the Genesis checkout.

## Layout of a run

`$GENESIS_VERIFY_ROOT` defaults to `${TMPDIR:-/tmp}/genesis-verify`.

| Path | Content | Removed by cleanup |
|---|---|---|
| `runs/<run-id>/bin/genesis` | CLI built from HEAD | yes |
| `runs/<run-id>/home/.local/share/genesis/modules/` | every module of `modules/`, installed with `genesis modules install` | yes |
| `runs/<run-id>/env/` | `spec.yaml` (from `assets/spec.yaml`), `genesis.lock`, `state/` (master key, secrets, `state.json`) | yes |
| `evidence/<run-id>/` | one transcript per command: `NN-<command>.txt` | **no** |

## Launch

```bash
.agents/skills/verify-genesis/scripts/launch.sh [run-id]    # default id: timestamp
```

It needs a running Docker daemon and Go. It builds the CLI, installs the 10 modules, writes the spec, runs `genesis init`, then the doctor. **Ready** = the doctor's last line is `ok`, and the run id is printed last (about 10 s with a warm Go cache). A short-lived CLI has no server to keep alive: every later command is a fresh process.

## Doctor

```bash
.agents/skills/verify-genesis/scripts/doctor.sh <run-id>
```

Read-only. Checks the binary and its version, that it was built from the current HEAD (otherwise launch a new run: you would be verifying stale code), that all modules are installed, that `state_dir` holds a master key, that Docker answers, and that no `genesis-*` container predates the run. Run it first whenever something looks off.

## Isolation

A run starts no container, so runs are independent of each other. The doctor still reports `genesis-*` containers that predate the run (left by `make test-docker` or another tool): they are not yours, never remove them.

## Drive

```bash
.agents/skills/verify-genesis/scripts/g.sh <run-id> <genesis arguments...>
```

Runs `genesis` from `runs/<run-id>/env` (so `-f spec.yaml` works) with the run's `HOME`, prints the transcript and saves it to `evidence/<run-id>/`. Its exit code is genesis's. Set `G_CWD=<dir>` to run elsewhere (needed by `modules scaffold`). Edit `runs/<run-id>/env/spec.yaml` to exercise other specs; copy it first if a journey needs the original back.

What to drive for each feature: [features/README.md](features/README.md).

## Evidence

- Each transcript holds the command, working directory, run id, commit, stdout, stderr and exit code. The age master key printed by `init` is replaced by `AGE-SECRET-KEY-<masked>`; `secrets get` output is replaced by its length. Never copy a secret into a report by another route.
- Prove behaviour through the user path: CLI commands, then a second read-only view of the result (`status`, `secrets list`, `modules verify`, `docker ps --filter name=genesis-`, files under `env/state/`). Exit code 0 alone is not proof.
- Check side effects, not only output: containers created and removed, `state.json` content, `genesis.lock` entries.
- A failing journey is reported as failing, with its transcript. Never edit the spec or the code to make a journey pass, and never report a step as verified through a different entry point.

## Cleanup

```bash
.agents/skills/verify-genesis/scripts/cleanup.sh <run-id>
```

Runs `genesis clean` through the CLI (transcript kept), removes any `genesis-*` container that appeared during this run and only those (listed in `evidence/<run-id>/leftover-containers.txt`: a run should start none, so report it), then deletes `runs/<run-id>`. Evidence stays in `evidence/<run-id>/`. Run cleanup after every run, including failed ones.
