# ADR-135 — Default modules per capability

- **Status**: accepted
- **Discussion**: [#135](https://github.com/WhiteRoseLK/genesis/issues/135)

Context: the spec makes `module:` optional and the resolver falls back to the module marked `defaults.<capability>: true` (`docs/04-spec.md`). Only `proxmox` and `fake-compute` declared a capability, so `time: {}` or `bastion: {}` was rejected with `no installed module provides it` (#126), and the minimal spec of doc 04 could not validate. Several modules provide the same function (`dns`: powerdns, coredns, bind; `pki`: vault, step-ca) and no decision had chosen the defaults.

Alternatives: require an explicit `module:` for every capability — rejected: the minimal spec and the `defaults` mechanism would disappear for no gain.

Decision: each module declares its capabilities in its manifest. The default modules are `time` → chrony, `dns` → powerdns, `pki` → vault, `secrets` → vault, `bastion` → teleport. coredns, bind and step-ca declare their capability without a default, so they stay selectable with `module:`. Two modules marked default for the same capability remain an error.

Consequences: manifests of chrony, powerdns, coredns, bind, step-ca, vault and teleport are updated, with no change to the core or the module contract. `test/integration/defaults_test.go` resolves the capabilities against the real manifests. `proxmox` and `fake-compute` both still mark `compute` as default; fake-compute is a test double (ADR-129) and is never installed alongside proxmox for the CLI. Modules added automatically through `requires` still ignore these defaults when several modules provide a required function (#136), so `bastion: {}` alone does not validate yet.
