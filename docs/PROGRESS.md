# Progress

| Milestone | Status | Tracking |
|---|---|---|
| M0 — Skeleton | done | [journal](journal.md) |
| M1 — Spec and validation | done | [journal](journal.md) |
| M2 — Secrets and state | done | [journal](journal.md) |
| M3 — SDK and module host | done | [journal](journal.md) |
| M4 — Resolver, broker, planner, engine | done | [journal](journal.md) |
| M5 — proxmox, base-os | done (partial) | Harden deferred ([#7](https://github.com/WhiteRoseLK/genesis/issues/7)) |
| M6 — chrony, coredns, powerdns | done | [journal](journal.md) |
| M7 — step-ca, vault | done | [journal](journal.md) |
| M8 — teleport, seed retirement | done | ADR-017 to 020; SSH repoint deferred ([#16](https://github.com/WhiteRoseLK/genesis/issues/16)) |
| M9 — Hardening, proof of extensibility | **in progress** | [M9 milestone](https://github.com/WhiteRoseLK/genesis/milestone/1) · issue [#2](https://github.com/WhiteRoseLK/genesis/issues/2) |

## Next step
M9: see the sub-issues of [#2](https://github.com/WhiteRoseLK/genesis/issues/2) and the [M9 milestone](https://github.com/WhiteRoseLK/genesis/milestone/1).

## Where to find the rest (ADR-049)
- **Current milestone**: GitHub milestone + parent issue labelled `milestone` + one sub-issue per PR.
- **Technical debt**: [`tech-debt` issues](https://github.com/WhiteRoseLK/genesis/issues?q=is%3Aissue+is%3Aopen+label%3Atech-debt).
- **Decisions**: [`09-decisions.md`](09-decisions.md) (index) and [`adr/`](adr/); discussions in the [`decision` issues](https://github.com/WhiteRoseLK/genesis/issues?q=is%3Aissue+label%3Adecision).
- **History**: [`journal.md`](journal.md) for M0 to M8; after that, the closing comment of each milestone's parent issue.
- **Details of each change**: PR descriptions and [`CHANGELOG.md`](../CHANGELOG.md).

## Updating
At the end of a milestone only: status in the table above, summary in the closing comment of the parent issue, GitHub milestone closed.
