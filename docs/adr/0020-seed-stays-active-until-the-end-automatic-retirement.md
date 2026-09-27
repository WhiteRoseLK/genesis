# ADR-020 — The seed stays active until the end, automatic retirement if everything is green

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Context: up to M7, the engine stopped a seed service (`SeedDown`) right after the handover of the matching function, in the middle of `apply`; docs 02/05 additionally planned a manual `genesis seed retire` command, never implemented. Behaviour wanted by the user: all seed services start; as soon as a real service is ready and verified (e.g. DNS), it serves every service built afterwards; the seed stays active for its own needs; it is decommissioned at the end, with no manual step, if every test is green.

Alternatives: (a) keep `SeedDown` during `apply`, with `seed retire` only finalising — rejected: no safety net, the seed disappears as soon as the handover happens; (b) `SeedDown` outside `apply`, triggered by a manual `genesis seed retire` — rejected by the user, the manual step adds nothing when the checks decide.

Decision: the handover (`Handover` + repoint) no longer touches the seed. At the end of `apply`, `internal/engine.retireSeed` stops the whole seed (in reverse plan order) if: every seed function has a target provider, the secrets have migrated to Vault when a `secrets` capability exists, and a final `Verify` (`verify.final`) of each target module is green. The state records `seed_retired`: a later `apply` no longer queries or restarts the seed modules, no longer replays `Handover`, and each function's active key points directly to the target. The `genesis seed retire` command is removed.

Consequence: the environment builds itself and becomes self-sufficient in a single `apply`. A seed function with no target successor keeps the seed (`apply` succeeds, and the log says so); a red final `Verify` makes `apply` fail with the seed intact. Switching the file store to read-only (doc 05) is not enforced by the code at this milestone (debt).
