# Spec validation and plan

A user checks a spec before applying it: `validate` checks structure, module resolution and each module's own `Validate`; `plan` shows the modules in execution order with their phase and why each one is there.

## Sub-features

- `validate-ok` accepts the baseline spec.
- `validate-rejects` rejects an invalid spec with an actionable message.
- `plan-order` lists modules in dependency order, with phase and source (declared or automatic).

## How to get to it (user POV)

- `genesis validate -f <spec>`
- `genesis plan -f <spec>`
- `genesis apply -f <spec>` prints the same plan before asking for confirmation.

## Driving it with g.sh

Preconditions:

- Launched run, baseline `spec.yaml`.

- **Valid spec.** Run `genesis validate -f spec.yaml`. Stdout: `spec.yaml: valid structure and module resolution.` then `all modules validated successfully.`; exit code `0`.
- **Plan.** Run `genesis plan -f spec.yaml`. Stdout starts with `Execution plan (8 modules):`; `fake-compute`, `chrony` and `powerdns` are `declared in spec`; `base-os`, `coredns`, `step-ca`, `vault`, `teleport` are `automatic (dependency)`; `coredns` and `step-ca` are in the `Seed (temporary container)` phase; every dependency appears before its dependents.
- **Rejected spec.** `cp spec.yaml spec.ok.yaml`, set `seed.address` to `192.168.1.10` in `spec.yaml`, run `genesis validate -f spec.yaml`. Exit code non-zero, stderr names `seed.address` and `network.cidr`. Restore with `mv spec.ok.yaml spec.yaml` and validate again.
- **Second view.** `plan` on the restored spec gives the same output as before the edit.

## Gotchas

- `time: {}` and the other module-less capabilities from the README do not resolve (`no installed module provides it`, #126). The baseline spec names every module on purpose; a journey that tests defaults reports #126.
- `validate` prints the structure line even when the module validation that follows fails: read the exit code and the last line.
- Without `NO_COLOR=1` (set by `g.sh`) the plan contains ANSI escapes.
