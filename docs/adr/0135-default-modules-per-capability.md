# ADR-135 — Default modules per capability

- **Status**: accepted
- **Discussion**: [#135](https://github.com/WhiteRoseLK/genesis/issues/135), [#136](https://github.com/WhiteRoseLK/genesis/issues/136)

Context: the spec makes `module:` optional and the resolver falls back to the module marked `defaults.<capability>: true` (`docs/04-spec.md`). Only `proxmox` and `fake-compute` declared a capability, so `time: {}` or `bastion: {}` was rejected with `no installed module provides it` (#126), and the minimal spec of doc 04 could not validate. Several modules provide the same function (`dns`: powerdns, coredns, bind; `pki`: vault, step-ca) and no decision had chosen the defaults.

Alternatives: require an explicit `module:` for every capability — rejected: the minimal spec and the `defaults` mechanism would disappear for no gain.

Decision: each module declares its capabilities in its manifest. The default modules are `time` → chrony, `dns` → powerdns, `pki` → vault, `secrets` → vault, `bastion` → teleport. coredns, bind and step-ca declare their capability without a default, so they stay selectable with `module:`. Two modules marked default for the same capability remain an error.

Consequences: manifests of chrony, powerdns, coredns, bind, step-ca, vault and teleport are updated, with no change to the core or the module contract. `test/integration/defaults_test.go` resolves the capabilities against the real manifests. `compute` keeps proxmox as its default (fake-compute declares the capability but is not the default: `defaults: { compute: false }`, ADR-129). Modules added automatically through `requires` use the same defaults: when several modules provide a required function, the one marked default for a capability it declares and another candidate also declares is chosen, so a module cannot win by inventing a capability (#136); without exactly one such module, the choice stays explicit.

Choosing vault for `pki` makes Vault, which holds the exportable root CA, the default PKI; step-ca remains available with `module: step-ca`. The default modules are listed in `docs/04-spec.md`; `test/integration/defaults_test.go` fails if that list and the manifests diverge.

The origin of each capability's module (explicit, default, only provider) is recorded by the resolver in `Resolved.CapabilitySource` and shown by `genesis plan` as a symbol in front of the capability name (`#name` default, `@name` only provider, bare name explicit; #140); a single candidate is reported as `only provider` even when it is flagged default.
