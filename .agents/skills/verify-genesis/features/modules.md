# Modules

A user installs modules from source, lists what is installed, checks installed binaries against the lock file, and scaffolds a new module from the repository root.

## Sub-features

- `modules-install` builds a module and installs it under `~/.local/share/genesis/modules/<name>/<version>/`, recording its digest in `genesis.lock`.
- `modules-list` lists installed modules with version and directory.
- `modules-verify` compares installed binaries with `genesis.lock`.
- `modules-scaffold` generates `modules/<name>/` that builds and validates.

## How to get to it (user POV)

- `genesis modules install <source-directory> [--lock-file <path>]`
- `genesis modules list`
- `genesis modules verify [--lock-file <path>]`
- `genesis modules scaffold <name> --provides <function,...>`, from the repository root.

## Driving it with g.sh

Preconditions:

- Launched run; `launch.sh` already installed every module (transcripts `01-…` to `10-…`).

- **Install.** Already exercised by launch. Each `NN-modules-install-<name>.txt` ends with `module <name>@<version> installed in … (digest …), genesis.lock updated.` and exit code `0`.
- **List.** Run `genesis modules list`. Ten lines, one per directory of `modules/`, each under `runs/<run-id>/home/.local/share/genesis/modules/`.
- **Verify the lock.** Run `genesis modules verify`. Exit code `0`. Then tamper: `echo x >> runs/<run-id>/home/.local/share/genesis/modules/chrony/0.1.0/module-$(go env GOOS)-$(go env GOARCH)` and run it again: non-zero exit naming `chrony`. Reinstall with `genesis modules install <repo>/modules/chrony` to restore.
- **Scaffold.** Use a throwaway worktree, never the working checkout: `git worktree add "$GENESIS_VERIFY_ROOT/runs/<run-id>/wt" HEAD`, then `G_CWD="$GENESIS_VERIFY_ROOT/runs/<run-id>/wt" scripts/g.sh <run-id> modules scaffold demo --provides demo.thing/v1`. The worktree gains `modules/demo/` with `module.yaml`; `genesis modules install <wt>/modules/demo` succeeds and `modules list` shows `demo`. Remove with `git worktree remove --force <wt>`.

## Gotchas

- `GENESIS_VERIFY_ROOT` is only set in your shell if you exported it; the default is `${TMPDIR:-/tmp}/genesis-verify`.
- `modules install` reports the French message `compilation de …` on build failure (#115): match on the exit code, not the text.
- The module binary name depends on the OS and architecture of the machine running the run (`module-darwin-arm64` on a Mac).
- Remove the scaffold worktree before `cleanup.sh`: `rm -rf` of the run leaves a dangling worktree entry otherwise (`git worktree prune` fixes it).
