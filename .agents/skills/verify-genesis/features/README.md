# Genesis verification map

The maintained list of what a Genesis user can do from the CLI, and how to prove each behaviour with the `verify-genesis` helpers. Read this index, then use the matching feature file as the recipe.

## Baseline preconditions

- A run launched with `scripts/launch.sh <run-id>` and a doctor that passes.
- The run's spec is `assets/spec.yaml`: fake-compute, chrony, powerdns, network `10.10.0.0/24`, seed `10.10.0.10`.
- Never drive a run you did not launch, nor a state directory outside `$GENESIS_VERIFY_ROOT/runs/`.

## Driving conventions

- Every command goes through `scripts/g.sh <run-id> …`. Commands below omit that prefix: `genesis plan -f spec.yaml` means `scripts/g.sh <run-id> plan -f spec.yaml`.
- Treat commands, flags and quoted messages as literal.
- After a mutation (`init`, `clean`, `modules install`, spec edit), check the result from a second, read-only command.

## Proof and skip reporting

- Proof = the transcripts in `evidence/<run-id>/` plus the read-only second view.
- Record the feature file and sub-feature ID with every transcript you cite.
- Report a journey that cannot run with the attempted command and the unmet precondition. A known failure is reported as failing, with the issue number.

## Features

- [Modules](./modules.md): list, install, verify the lock, scaffold a new module.
- [Spec validation and plan](./validate-plan.md): `validate` and `plan`, valid and rejected specs.
- [Seed state](./seed-state.md): `init`, `status`, `secrets list|get`, `clean`.
