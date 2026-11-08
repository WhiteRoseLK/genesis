# Spec validation and plan

A user checks a spec before applying it: `validate` checks structure, module resolution and each module's own `Validate`; `plan` shows the modules in execution order with their phase and why each one is there.

## Sub-features

- `validate-ok` accepts the baseline spec.
- `validate-rejects` rejects an invalid spec with an actionable message.
- `default-modules` resolves a capability without `module:` to its default module (ADR-135).
- `plan-order` lists modules in dependency order, with phase and source (declared or automatic).

## How to get to it (user POV)

- `genesis validate -f <spec>`
- `genesis plan -f <spec>`
- `genesis apply -f <spec>` prints the same plan before asking for confirmation.

## Driving it with g.sh

Preconditions:

- Launched run, baseline `spec.yaml`.

- **Valid spec.** Run `genesis validate -f spec.yaml`. Stdout: `spec.yaml: valid structure and module resolution.` then `all modules validated successfully.`; exit code `0`.
- **Plan.** Run `genesis plan -f spec.yaml`. Stdout starts with `Execution plan (8 modules):`; `fake-compute` (`compute`), `chrony` (`time`) and `powerdns` (`dns`) are `declared in spec`; `base-os`, `coredns`, `step-ca`, `vault`, `teleport` are `automatic (dependency)`; `coredns` and `step-ca` are in the `Seed (temporary container)` phase; every dependency appears before its dependents.
- **Default modules.** The baseline gives no `module:` for `time` and `dns`. In the `plan` output, `chrony` reads `declared in spec (time)` and `powerdns` reads `declared in spec (dns)`.
- **Minimal spec.** `cp spec.yaml spec.ok.yaml`, replace the `time` and `dns` lines by `bastion: {}`, run `genesis validate -f spec.yaml` (exit `0`) then `genesis plan -f spec.yaml`: `teleport` is `declared in spec (bastion)`; `chrony`, `powerdns` and `vault` are `automatic (dependency)`. Restore with `mv spec.ok.yaml spec.yaml`.
- **Rejected spec.** `cp spec.yaml spec.ok.yaml`, set `seed.address` to `192.168.1.10` in `spec.yaml`, run `genesis validate -f spec.yaml`. Exit code non-zero, stderr names `seed.address` and `network.cidr`. Restore with `mv spec.ok.yaml spec.yaml` and validate again.
- **Second view.** `plan` on the restored spec gives the same output as before the edit.

## Gotchas

- Capabilities without `module:` resolve to their default module (ADR-135), including for modules added through `requires`: `bastion: {}` alone validates and plans (#136).
- `validate` prints the structure line even when the module validation that follows fails: read the exit code and the last line.
- Without `NO_COLOR=1` (set by `g.sh`) the plan contains ANSI escapes.
