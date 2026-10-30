# Seed state

A user initialises the seed (state directory and master key), inspects the state per module, lists and reads generated secrets, and purges intermediate artefacts.

## Sub-features

- `init` creates `state_dir` with an age master key and prints the key once.
- `status` shows the secrets backend, the seed status and each module's state.
- `secrets` lists secret metadata (`list`) and returns one value (`get <reference>`).
- `clean` purges intermediate secrets and resets the state, keeping the master key.

## How to get to it (user POV)

- `genesis init [-f <spec>]`
- `genesis status [-f <spec>]`
- `genesis secrets list`, `genesis secrets get <reference>` (state directory from `--state-dir`)
- `genesis clean [-f <spec>] [--auto-approve]`

## Driving it with g.sh

Preconditions:

- Launched run (launch ran `init`).

- **Init.** Read `evidence/<run-id>/11-init-spec-yaml.txt`: `Master key generated. Back it up now…`, the key masked, `state_dir ready: …/env/state`, exit `0`. Second view: `ls -l runs/<run-id>/env/state` shows `master.key` with mode `-rw-------`.
- **Init is idempotent.** Run `genesis init -f spec.yaml` again: exit `0`, no new key printed, `master.key` unchanged (`shasum` before and after).
- **Status.** Run `genesis status -f spec.yaml`: `Secrets backend: file`, `Seed: active`, then one line per module once an `apply` has written state (`(none)` before).
- **Secrets.** Run `genesis secrets list --state-dir "$GENESIS_VERIFY_ROOT/runs/<run-id>/env/state"`. Before any `apply` the list is empty. After a successful `apply`, pick one reference and run `genesis secrets get <reference> --state-dir …`: exit `0`, transcript shows only `<secret value masked, N bytes>`.
- **Clean.** Run `genesis clean -f spec.yaml --auto-approve`: `reset state in …/state.json`, `clean complete.` Second view: `master.key` still present, `secrets/` empty.

## Gotchas

- `init` prints the master key on stdout by design. Only `g.sh` transcripts mask it: never run `genesis init` outside `g.sh`, never paste raw output.
- `secrets` takes the state directory from `--state-dir`, not from `-f`.
- Meaningful `status` and `secrets` journeys need a successful `apply`, which is blocked by #124.
