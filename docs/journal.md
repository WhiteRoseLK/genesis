# Milestone journal (M0 to M8)

> Frozen history. Since M9, the summary of each milestone is the closing comment of its parent issue on GitHub (ADR-049).

Detailed history of each milestone (done, decisions, debt, next step), as written at the end of the milestone. The current state is in [`PROGRESS.md`](PROGRESS.md); open debt is tracked in the [`tech-debt` issues](https://github.com/WhiteRoseLK/genesis/issues?q=is%3Aissue+is%3Aopen+label%3Atech-debt). Milestones were originally named J0–J9 (French *jalon*); they were renamed M0–M9 when the project switched to English (ADR-054).

<!-- One entry per milestone, added at the end of the milestone: date, milestone, done, decisions, debt (with issue numbers), next step. -->

### 2026-09-17 — M0 Skeleton
**Done**
- Root module `genesis` (`cmd/genesis` + `internal/cli`): a cobra CLI exposing the 9 commands of the doc 02 table (`init`, `modules list/install/verify`, `validate`, `plan`, `apply`, `status`, `secrets list/get`, `seed retire`, `destroy`), each a "not implemented yet (milestone Mx)" stub until the matching milestone is done.
- Separate `genesis/sdk` module (`sdk/go.mod`) with a placeholder `sdk/go/doc.go`; `sdk/proto/buf.yaml` + `buf.gen.yaml` ready for the `module/v1` protocol (M3), a no-op for now (no `.proto` file).
- `go.work` (workspace `.` + `./sdk`).
- Apache 2.0 `LICENSE`, `NOTICE`, SPDX headers on every `.go` file.
- `Makefile`: `build`, `test`, `lint` (iterating over `GO_MODULES := . sdk`, to be extended as modules arrive), `proto` (no-op while there is no `.proto`), `e2e` (`integration` build tag, never run without an explicit request).
- `.golangci.yml`: a `depguard` rule with two lists — `internal/`+`cmd/` must never import `modules/`; `sdk/` must never import `internal/` or `cmd/`. **Checked by hand in both directions** (a temporary forbidden import, confirmed blocked by `golangci-lint run`, then reverted before committing): the mechanism is proven, no proof committed (no real module yet for an automated test — coming in M4 with `fake-compute`/`test-a..d`).
- `.github/workflows/ci.yml`: `test` (build + test) and `lint` (golangci-lint, including the import rules) jobs on GitHub push/PR.
- Tooling installed in the dev environment: Go 1.27.1, golangci-lint 2.13.2, buf 1.73.0, protoc-gen-go, protoc-gen-go-grpc.
- `make build lint test proto` green.

**Decisions**
- Go module name: `genesis` (root) / `genesis/sdk` — no domain (e.g. `github.com/...`) until the real GitHub repository is created; to be renamed with one command when the time comes.
- Development CI (lint/test) on **GitHub Actions**; the app's deployment CI/CD will be on **GitLab**, outside this repository's scope.
- M0 import rule implemented with `depguard` (golangci-lint), no home-made script — reuses the lint tooling already in place.
- **Dependency license check: removed from the M0 scope** at the user's explicit request, even though the criterion exists in doc 08. No formal ADR (not an architecture choice, just a postponed task) — noted here as debt so it stays traceable against the document's acceptance criterion.

**Debt**
- Dependency license check in CI: not implemented (see decision above). To do before the repository goes public or heavy dependencies are added.
- The `modules ↛ internal` import rule was only checked by hand (no real module in the repository for a permanent automated test). To be secured by an automated conformance test once `test/modules/` exists (M4).

**Next step**: M1 — Spec and overall validation (doc 04).

### 2026-09-17 — M1 Spec and overall validation
**Done**
- `internal/spec`: Go types (`Environment`, `Metadata`, `Network`, `Seed`, `Size`, `Capability`), each capability's `config` left opaque (`map[string]any`) — its module-specific validation is delegated to M3/M4.
- `schema.json` (embedded, draft 2020-12) validates the overall envelope: required fields, `apiVersion`/`kind` as `const`, `profile` as `enum: [connected]` (ADR-008), sizes, capabilities (`minProperties: 1`), placement; `additionalProperties: false` wherever the shape is closed.
- `refs.go`: every `*_ref` key must be `env://`, `file://` or `vault://` (doc 04, rejection of literal secrets), checked recursively.
- `defaults.go`: `profile=connected`, `seed.state_dir=/var/lib/genesis`, `seed.container_runtime=auto`.
- `Load(path)`: read → generic decode → JSON round trip (native types expected by `jsonschema`) → schema validation (aggregated errors, `a.b.c` path, with special handling of `missing properties` errors so that the path points to the missing field rather than its parent) → `_ref` validation → typed decode → defaults.
- `genesis validate -f` runs this structural validation (module resolution and per-module `Validate` remain M3/M4 stubs).
- Tests: both doc 04 examples (full and minimal) load without error; 13 invalid fixtures (`testdata/invalid/`) each fail with the expected YAML path in the message.

**Decisions**
- ADR-015: `santhosh-tekuri/jsonschema/v5` for JSON Schema validation (reusable as is for module `config_schema` in M3).

**Debt**
- None new. Per-module `config_schema` validation and capability→module resolution remain outside the M1 scope, as planned by doc 08.

**Next step**: M2 — Secrets and state (doc 06).

### 2026-09-17 — M2 Secrets and state
**Done**
- `internal/secrets`: `Secret` (redaction in `String`/`MarshalJSON`/`LogValue`), `Ref` (anti-traversal validation), `Store` interface (doc 06), generators (32-char password, 256-bit token, ECDSA P-384 key, Ed25519 key, Ed25519 SSH pair — certificate generator deferred, `pki.issuer` does not exist before M7), `MasterKeyProvider` (ADR-007) + `file` implementation (`filippo.io/age` X25519 identity in `state_dir/master.key`, generated once by `init`), `FileStore` (age-encrypted secret + separate plaintext metadata, atomic writes, `0700`/`0600` permissions).
- `RedactingHandler`: wraps any `slog.Handler`, redacts `Secret` values and known patterns (Vault tokens, PEM blocks) in the message and the attributes, including nested ones.
- `internal/state`: `State{SchemaVersion, SecretsBackend}` — deliberately minimal, the rest (VMs, module statuses, endpoints...) will come with the milestones that produce it. Atomic `Load/Save`; `Lock` through a non-blocking `flock` on `state_dir/state.lock`.
- CLI: persistent `--state-dir` flag (default `/var/lib/genesis`); `genesis init` generates/loads the master key (displayed only once), idempotent; `genesis secrets list` (metadata only) and `genesis secrets get <ref>` (reveals the value — that is the command's purpose) wired to the `file` backend.
- Tests: `Ensure` idempotent (secrets); two concurrent `Lock`s on the same `state_dir` → the second fails immediately (two distinct descriptors, faithfully simulating two processes); redaction test (Vault token, PEM block, `Secret` value, including through `logger.With` and a formatted message) — nothing leaks, all green.

**Decisions**
- `--state-dir` as a persistent flag on the root (absent from the doc 02 CLI table, but necessary: `init` must know where to work before a spec exists) — same default as `seed.state_dir` in M1.
- `secrets get` prints the plaintext value on standard output: that is the command's very purpose (retrieval requested by the operator), distinct from the "no plaintext secret" rule, which targets unwanted leaks (logs, state, errors, `secrets list`).
- State lock implemented with `syscall.Flock` (stdlib, Linux-only target per doc 01) rather than an external dependency.

**Debt**
- None new. Certificate generator (`pki.issuer`) deferred to M7 as planned, the function does not exist before.

**Next step**: M3 — SDK and module host (doc 03).

### 2026-09-17 — M3 SDK and module host
**Done**
- `sdk/proto/module/v1/module.proto`: `Module` service (the 11 RPCs of doc 03), `config`/`state` as `google.protobuf.Struct` (native opaque JSON). Package `module.v1` (no `genesis.` prefix) so that the directory matches the buf convention. Three STANDARD rules excepted in `buf.yaml` (`SERVICE_SUFFIX`, `RPC_REQUEST_RESPONSE_UNIQUE`, `RPC_REQUEST/RESPONSE_STANDARD_NAME`): the document deliberately defines a `Module` service (not `ModuleService`) and `StepRequest`/`StepResult` shared by the whole lifecycle. `make proto` now really generates code.
- `sdk/go`: `Serve(modulev1.ModuleServer)` (go-plugin, shared handshake `sdk.Handshake`/`sdk.ClientPlugins()`), `LoadManifest`/`ParseManifest`/`ManifestFile.ToProto()` (`module.yaml` parsing, short/long forms of `requires`).
- `sdk/go/moduletest`: `RunConformance` — a skeleton (Describe consistent with the manifest, Validate does not crash). Full semantic suite (replayed idempotence, secrets absent from outputs, `UpsertRecord`→`ListRecords`) deferred to M4 (needs the broker).
- `internal/modulehost`: `Discover` (search directories, `<name>/<version>/{module.yaml, module-<os>-<arch>, assets/}` layout), `Fingerprint` (SHA-256), `Launch`/`Client` (go-plugin, logger at Warn) with `WrapModuleError` to turn a module crash into a clean error.
- `internal/modulelock`: `genesis.lock` (name → {version, sha256}), `Verify` refuses a mismatching digest.
- `internal/scaffold`: `Generate` writes a complete `modules/<name>/` (go.mod, main.go with stubs for each RPC, module.yaml, schema.json, conformance test), `go work use`, validates with `go build` — **without** `go mod tidy` (see decision below).
- CLI: `genesis modules list/install/verify/scaffold` implemented (no longer stubs).
- `test/modules/panicking`: a test module whose `Check` panics, used for the supervision test and for the end-to-end scaffold→install→Discover→Launch→Describe chain.
- Makefile fix (M0): `GO_MODULES` discovered with `find . -name go.mod` instead of a hard-coded list — otherwise every added module would have required editing the Makefile, violating the non-negotiable rule "adding a module must not require any change outside its directory".
- New `depguard` rule `modules-must-not-import-core` (see decision below).
- `internal/atomicfile` extracted from `internal/secrets`/`internal/state` (rule of three: `internal/modulelock` needed it too).

**Decisions**
- **No explicit `require genesis/sdk` in a module's go.mod, no automatic `go mod tidy`.** Under `go.work`, a `require <monorepo-module-without-a-real-domain> vX` pushes `go build`/`go mod tidy` to attempt a network resolution ("malformed module path" or a DNS fetch attempt) rather than use the workspace substitution — checked empirically (see session history). Without this line, importing a package that belongs to another `use`d module resolves locally, with no network and no `go.sum`, which is documented `go.work` behaviour. `scaffold`/`install` therefore validate with `go build` only.
- `genesis/sdk` stays named without a real domain (see the M0 decision); the point above is the concrete consequence of that choice, now well understood.
- `modules-must-not-import-core` (depguard): largely redundant with Go's native rules (a package under `internal/` cannot be imported outside the tree of the module that owns it; `cmd/genesis` is `package main`, never importable) — checked empirically in both cases, the reported error is `typecheck`, not `depguard`. Kept anyway as documentation/a safety net should a core package ever leave `internal/`.
- `genesis modules install` builds the module from its source directory (`go build`) rather than expecting a prebuilt binary — matches the doc 10 example (`genesis modules install ./modules/<name>`).
- `--lock-file` (default `genesis.lock`) added to the `modules install`/`verify` commands: `genesis.lock` lives "next to the spec" (doc 02) but no command has a notion of a spec working directory before M4.

**Debt**
- No new latent debt: the certificate generator and the full semantic conformance suite were already noted as deferred (M7/M4) in earlier milestones.

**Next step**: M4 — Resolver, broker, planner, engine (docs 02, 05).

### 2026-09-18 — M4 Resolver, broker, planner, engine
The most complex milestone so far. Full details of the decisions are in the commit messages (`git log --oneline` from "feat: function protocols and bidirectional broker plumbing" to "feat: wire genesis plan/apply"); summary here.

**Done**
- **Concrete functions**: `functions/core/secrets/v1` (native, never a module), `functions/compute/vm/v1` (provided by `fake-compute`), `functions/test/echo/v1` (generic, reused by the test modules under different names).
- **SDK**: `sdk.Serve` accepts `FunctionProvider`s (a provided function = a `function:<name>` plugin dispensed in addition to the lifecycle); `BrokerAware`/`BrokerClient` let a module dial, during a step, the go-plugin subchannel opened by the core (`StepRequest.broker_token`) — this is the bidirectional call mechanism (like Terraform provisioners), the mirror of the usual core→module direction.
- **`internal/broker`**: `Registry.BuildSession(caller, allowed)` builds a `grpc.Server` that registers only the declared functions — an undeclared function is simply never registered, so the call fails with `Unimplemented` **architecturally**, not through an ad hoc check. `core.secrets/v1` has a native implementation with access control (owner/consumers, through `secrets.Store.GetMeta`, added for the occasion). `compute.vm/v1` and `test.*/v1` are relayed to the active provider module (`ForwarderFor`).
- **`internal/resolver`**: capability → module (explicit, sole installed candidate, or `defaults.<capability>`), closure over required functions up to a fixed point (automatic additions), `core` compatibility (home-made semver comparator, no new dependency). `FunctionProviders` is **per phase** (`function → phase → module`): a function can have a seed provider and a different target provider at the same time (doc 05), which is not a conflict.
- **`internal/planner`**: module-granularity DAG (edges derived from required/provided functions), deterministic Kahn topological sort, cycle detection.
- **`internal/engine`**: launches every resolved module, registers their provided functions in the broker, then for each module: a single `Check` decides whether to replay its group of steps (seed.up if applicable, provision, configure, verify, then handover+seed.retire when there is a handover) — an accepted simplification compared with one `Check` per individual RPC, safe because each step is itself idempotent (doc 03 §4 rule 3). State persisted after each successful step.
- **Real modules**: `modules/fake-compute` (in-memory registry, no systemd/SSH containers for this milestone); `test/modules/{test-a,test-b,test-c,test-d}` — test-a exercises seed+handover, test-b/c/d really call their dependency through the broker during `Verify`.
- **CLI**: `genesis plan`/`genesis apply` wired for real (resolution, plan shown with automatically added modules marked, y/N confirmation unless `--auto-approve`, execution through the engine).
- The 5 acceptance criteria checked individually by test name (see session): correct order, cycle detected, undeclared function refused, kill+re-run→resumption, second apply→0 changes, plus the proof of extensibility with `test-d`.

**Decisions**
- Real bug found and fixed along the way: go-plugin's `GRPCBroker.AcceptAndServe` is **blocking** (like `http.Server.Serve`); calling it synchronously in `OpenSession` froze the whole engine from the first step. Fixed by running it in a goroutine — `Dial` on the module side already waits up to 5 s for the connection info, no extra synchronisation needed.
- Broker genericity without a per-module core change: each "official" function type (defined once in the SDK) has a forwarder written once in `internal/broker`; adding a **module** never touches this file, only adding a **new function** (rare, at SDK level) would — consistent with the scope of the non-negotiable rule.
- No explicit `Repoint` choreography sent to consumers: in this architecture, a consumer opens a new broker session for each function call, so changing the active provider in the registry is enough to "repoint" without a dedicated RPC. The full multi-module choreography of doc 05 (real DNS/PKI handover) remains to be built when the real modules (M6/M7) need it.
- `internal/state.State` gains a `Modules` field (opaque state per module, `StepResult.state` persisted) — this is the milestone that actually produces it, not a speculative addition.
- `secrets.Store` gains `GetMeta` — needed for the access control of `core.secrets/v1`, absent until now because nothing needed it.

**Debt**
- `internal/engine` replays the complete group of steps of a non-compliant module rather than resuming exactly at the failed RPC — accepted, safe thanks to idempotence, but less granular than doc 02 seems to imply ("apply resumes at the first non-compliant step").
- Multi-module handover choreography (explicit Repoint, file→vault secret migration) not built: deferred to M6/M7 with the real DNS/PKI modules.
- The spec's resolved config is not yet passed to the module (`StepRequest.config` is always empty): no real module needs it yet; to be wired with proxmox (M5).

**Next step**: M5 — `proxmox` and `base-os` modules (doc 07).

### 2026-09-18 — M5 `proxmox` and `base-os` modules
First milestone touching ground that cannot be verified in this environment (no real Proxmox cluster — confirmed with the user before starting) and first to exercise real Ansible content (Docker is available on this machine and used for real).

**Scope decision (user)**: `os.base/v1` separates functional configuration (what the VM needs to work — CA, resolver, NTP) from pure hardening (SSH, nftables — security). Only the first half is built at this milestone; `Harden` is an explicit stub (`Unimplemented`, clear message), not a half-written feature. Quote (translated): *"forget pure hardening for now, it will be a feature later [...] you can test everything the VM needs to work (DNS, NTP, CA, etc.)"*. By construction, the literal doc 08 criterion "VM... hardened" is therefore only met for its functional side, not its security side — an accepted decision, not an oversight.

**Done**
- `internal/runner.ContainerRuntime`: drives docker/podman through the CLI (`seed.container_runtime`), no third-party Docker SDK. Tested for real against Docker on this machine (blocking run + exit code, detached run + stop + status, host mount).
- `core.container/v1` and `core.ansible/v1` (native): `internal/broker/container.go`, `ansible.go`. `RunPlaybook` writes the playbook/inventory/vars into a shared directory mounted into a `willhallonline/ansible` container.
- **Real bug found and fixed**: the shared working directory must be world-readable (`0755`, not the default `0700` of `MkdirTemp`) — the ansible container runs as its own internal UID, distinct from the caller's (confirmed here through a user namespace offset, but the problem is general, not specific to this environment).
- **Real integration test** (`internal/broker/ansible_test.go`): a disposable SSH container (`linuxserver/openssh-server`) as the target, playbook run for real, checked through `docker exec` (not by trusting ansible). Two environment limits documented in the test: the docker0 bridge network is not directly reachable from the test process here (only from another container, or through the daemon) — checked with `docker exec`, not a direct dial; the target image has no Python — test playbook in `ansible.builtin.raw` (the real target VMs, a Debian cloud image, have it).
- `modules/base-os`: real `TrustCA`/`SetResolver`/`SetNTP` (embedded Ansible playbooks), `Harden` stub. **Real bug #2**: `broker.Dial(token)` only succeeds once per session token (the connection info goes through a single-use channel on the core side) — calling `TrustCA` then `SetResolver` while re-dialling the same token each time blocked the second call. Fixed by caching the connection dialled once, reused for all later function calls.
- `modules/proxmox`: hand-written Proxmox VE REST client (`proxmoxapi/`), tested against `httptest` fixtures faithful to the documented API (no third-party SDK — the surface needed is narrow and in any case cannot be verified live here). `EnsureVM` idempotent by name, `DeleteVM` idempotent. **Accepted scope**: `EnsureImage` assumes a template already exists (no automatic cloud image download + conversion — the most complex chain and the least verifiable without a real cluster).
- **Real bug #3, found by the fixture test itself**: a Proxmox clone carries the *template* ID in the URL path and the *new* ID in a `newid` form field, not the other way round — the client (`proxmoxapi`) already had it right, it was the test fixture that was wrong; caught only because the test exercises the real request shape end to end rather than mocking at each method's boundary.
- **M4 gaps filled along the way** (exactly the method announced by the user, translated: "as the tests go, if something mandatory is missing, we add it"):
  - `resolver.Module.Config`: the spec's resolved config (doc 04, merged when several capabilities point to the same module) is now passed in `StepRequest.config` — M4 always left it empty, since no module needed it.
  - `internal/engine.resolveRefs`: `*_ref` fields (`env://`, `file://`, `vault://`) are resolved to real values before reaching a module (`vault://`: a clear "not supported yet, M7" error, not a silent failure).
  - `compute.vm/v1.EnsureVMRequest` gains `ip`, `gateway`, `ssh_public_key`, `user` (cloud-init, doc 07) — an additive change, not a breaking one.
- SDK conformance suite (`moduletest.RunConformance`) wired for `proxmox` and `base-os` (hand-written modules do not have one by default, unlike those generated by `scaffold`) — an explicit acceptance criterion of doc 08.

**Debt**
- `EnsureImage` (proxmox): no automatic template download/conversion — to be built once it can be validated against a real cluster (M9 or an explicit request).
- `Harden` (base-os): scope decision above, not built yet.
- nftables and resolver/NTP really wired into `Repoint` (not only on the initial call): not exercised yet — depends on real DNS/PKI modules (M6/M7).
- IP allocation from the pool (doc 04 `network.pool`): not built yet; `EnsureVMRequest.ip` is provided by the caller for now, not allocated by the core.
- `proxmox`/`base-os` never run against real infrastructure — only HTTP fixtures and disposable containers.

**Next step**: M6 — `chrony`, `coredns`, `powerdns` modules (doc 07).

### 2026-09-18 — M6 `chrony`, `coredns`, `powerdns` modules
First milestone to exercise a real multi-module handover (docs/05-bootstrap-lifecycle.md): `coredns` (seed) → `powerdns` (target) on the same `dns.zone/v1`/`dns.resolver/v1` functions. The mechanism did not exist yet in `internal/engine` (only a module handing over to itself, `test-a`, was exercised) — built at this milestone rather than guessed in advance, in line with the method already approved by the user ("we add things as the tests go").

**Done**
- `modules/coredns`: `dns.zone/v1` + `dns.resolver/v1` in the seed phase, a real CoreDNS container on the seed (`core.container/v1`), BIND zone regenerated on every `UpsertRecord`/`DeleteRecord`. DNS resolution proven for real (query from a third-party container, not `docker exec` inside CoreDNS itself). Became a real engine-driven module during this milestone: `Check` now distinguishes `TODO`/`COMPLIANT` (`seeded`/`retired`), `SeedUp` bootstraps, `SeedDown` really stops the container.
- `modules/chrony`: first target module owning its own full VM lifecycle (`compute.vm.EnsureVM` + SSH pair through `core.secrets`, `Configure` installs/configures a `chronyd` server through `core.ansible` with its own playbook, `Verify` measures a real offset (`chronyc tracking`) from a disposable third-party VM, threshold < 100 ms, `Destroy` deletes the VM).
- `modules/powerdns`: `dns.zone/v1` + `dns.resolver/v1` in the target phase — Authoritative (SQLite, API) + Recursor on its own VM. `api.go` talks directly to the PowerDNS REST API (same principle as `modules/proxmox/proxmoxapi`, no third-party SDK). `Configure` really consumes `time.ntp/v1` and `dns.resolver/v1` (chrony/coredns, both real at this stage — unlike chrony, which had no provider to build yet). `Handover` reads `dns.zone/v1@seed` (coredns) back, recreates each record through its own implementation, compares. `Verify` proves forward + reverse resolution + an external name (recursor) from a disposable third-party VM.
- **`internal/engine` restructured for real cross-module handover**: providers registered under a qualified key (`function@phase`) in addition to the active key (no suffix); a pure seed module (no target phase, e.g. coredns) stops at `seed_ready` (`SeedUp`+`Verify`) instead of trying `provision`/`configure`/`handover`; `handoverSeedModules` detects, for each function provided in the target phase, an active seed provider — the same module (self-handover, `test-a`) or a different one (`coredns`→`powerdns`) — and triggers `SeedDown` on **each** of them, deduplicated; `repoint` makes the target module the new active provider after a successful handover, without re-dispensing.
- Missing broker forwarders added: `os.base/v1`, `dns.zone/v1`, `dns.resolver/v1`, `time.ntp/v1` (only `compute.vm/v1` had one; no module consumed the others through the real broker before this milestone).
- New fixtures `test/modules/test-e` (pure seed) / `test-f` (target, reads `test.e/v1@seed`): proof, outside real products, that the cross-module handover mechanism routes `Handover` to the right module and `SeedDown` to the **other** one — `internal/engine/handover_test.go`.
- Each module tested in line with rule 7 of doc 03 ("testable on its own, required functions simulated"): the real transport mechanism (`core.ansible/v1` through Docker) is proven once and for all in M5, and each module then tests its own wiring with simulated providers — except `coredns` (DNS resolution proven for real, cheap) and `powerdns.api.go` (real HTTP round trip through `httptest`, independent of Docker).

**Decisions**
- ADR-016: `core.ansible/v1` open to any target module, not reserved for `base-os` — `chrony`/`powerdns` declare their own product playbook, `os.base/v1` stays reserved for the common configuration (CA, resolver, NTP client).
- The proto's explicit `Repoint(RepointRequest)` is not triggered yet by the core towards consumer modules: in this MVP, no module consumes `dns.zone/v1`/`dns.resolver/v1`/`time.ntp/v1` without a phase suffix beyond its own `Check` (the only consumer sensitive to the handover, `powerdns`, explicitly reads `dns.zone/v1@seed`, a stable key the handover never changes). The "active" registry key (repoint) is nevertheless really updated after each handover — the infrastructure is in place, simply not yet observed by a real consumer.
- `os.base.TrustCA` is still not called by `chrony`/`powerdns` (no CA before `step-ca`/M7); `SetNTP`/`SetResolver`, on the other hand, are now really exercised by `powerdns` (chrony and coredns already exist at this point of the milestone).

**Debt**
- Like `coredns`, the connection parameters of `powerdns` (VM address, API key) live in memory in the module process, populated by `Configure` — lost if the core restarts mid-lifecycle (same debt, same justification: `StepResult.state` is not passed to the `dns.zone/v1` function handlers, only to lifecycle steps).
- No automatic reverse zone/PTR derivation: `dns.zone/v1` accepts any record type (PTR included) generically, but nothing automatically creates the `in-addr.arpa` zone matching an A record — `powerdns`'s `Verify` does it by hand for its own probe.
- Explicit `Repoint(RepointRequest)` not wired on the core side (see decision above) — to be built as soon as a module consumes a function subject to handover without picking up the session token on each call (see also the following nuance).
- `chrony`/`coredns`/`base-os` cache their broker connection from the **first** token received (`Check`) and reuse it for their whole lifecycle, whereas `internal/engine` provides a fresh token at each step (`test-b`/`test-f` redial with the current call's token, the idiomatic pattern). No consequence here (none of these modules consumes a function subject to handover after its own `Check`), but to be fixed — dial with `req.GetBrokerToken()` on each call, not a cached token — before an existing module consumes a function subject to repoint (`vault`/`openssh-bastion`, M7/M8).
- None of the three modules has run through `internal/engine.Run()` on a real multi-module Docker cluster end to end (coredns+chrony+powerdns+fake-compute+base-os together) — each module is proven in isolation (required functions simulated) plus the handover mechanism proven generically (`test-e`/`test-f`). The real multi-module proof is the explicit scope of M9 (doc 08).

**Next step**: M7 — `step-ca`, `vault` modules (doc 07).

### 2026-09-19 — M7 `step-ca`, `vault` modules
The heaviest milestone of the project so far, split into two steps approved separately by the user (step-ca first, a checkpoint, then vault). First milestone to exercise a real PKI hierarchy (root → intermediate → third-party intermediate) and a secret migration orchestrated by the core.

**Done**
- `modules/step-ca`: `pki.issuer/v1` in the seed phase — `IssueCert`/`SignCSR`/`CAChain` really drive the `step` CLI of the `smallstep/step-ca` container (local/offline mode: `step certificate create`/`sign`, no detached network server — avoids all the TLS/DNS/provisioner complexity of the API mode, unnecessary as long as nothing consumes step-ca's own HTTP API). `SignSSH`: an explicit `Unimplemented` stub, deferred to `openssh-bastion` (M8), same method as `Harden` in `base-os` (M5).
- **Real bug found while preparing vault**: step-ca's default root (`--profile root-ca`, pathlen:1) only allows signing one level of intermediate — not enough for `vault` to get its own `pki_int` (one more intermediate in the chain) signed by step-ca. Fixed: root generated from a template (`--template`, the only way to set `maxPathLen` on a self-signed root) with `maxPathLen: 2`; `SignCSRRequest` gains `is_ca`/`path_len_constraint` (additive change) to request an intermediate rather than a leaf.
- `modules/vault`: `pki.issuer/v1` + `secrets.kv/v1` in the target phase — single-node Raft, initial TLS via `pki.issuer/v1@seed` (step-ca signs vault's server certificate), `pki_int` signed by step-ca (exactly the scenario of the bug above), KV v2, `genesis` AppRole. `api.go` talks directly to the Vault REST API (same principle as `modules/powerdns/api.go`, `modules/proxmox/proxmoxapi`).
- `Handover` (vault): reissues its own TLS certificate through its own `pki_int` (not step-ca's), redeploys, revokes the root token (doc 07).
- **`file` → `vault` migration** mechanism built in `internal/engine` (`migrateSecretsIfNeeded`): as soon as the module chosen for the `secrets` capability provides `secrets.kv/v1` and becomes `target_ready`, each non-`recovery` secret is copied, verified by reading it back, then `state.SecretsBackend` switches — this is not a classic function handover (`secrets.kv/v1` has no seed provider to take over from; it is `core.secrets/v1`, native, that changes backend), hence a trigger separate from `handoverSeedModules`/`repoint` (M6). Proven generically with a new `test-kv` fixture (in-memory KV), like `test-e`/`test-f` for the cross-module handover.
- New `secrets.kv/v1` proto (`Read`/`Write`/`List`) + broker forwarder; `pki.issuer/v1` forwarder added.
- `step-ca` tested against the real container (real issuance + CSR signing, chain verified cryptographically up to the root, idempotence proven across a module restart, and signing of a third-party intermediate able to sign its own leaf — exactly the scenario `vault` needs).
- `vault` tested against a real `hashicorp/vault` container and a real `step-ca`: Vault needs a real Debian systemd+apt for its real installation (unlike chrony/coredns/powerdns, out of reach of `fake-compute`'s Alpine containers) — the test's fake `core.ansible/v1` therefore drives Docker directly with the REAL variables (certificates, `role_id`/`secret_id`) the module passes to it, to exercise everything else for real (init, unseal, `pki_int`, KV, AppRole, Handover, Verify).
- **Four more real bugs found by testing `vault` against a real server**: Raft election delay after unsealing even with a single node ("local node not active"); AppRole policy without access to `pki_int/issue` (`IssueCert` failed with a 403 with the AppRole token, only tested with the root token until then); IP SAN sent in `alt_names` (DNS) instead of `ip_sans` (silently ignored by Vault); intermediate and leaf lifetimes confused (an issuer can never sign a certificate that expires after itself); re-unsealing needed after each service restart (Vault's seal state always lives in memory, never persisted, including during `Handover`).

**Decisions**
- Explicit split requested by the user: step-ca fully built and tested before starting vault, with a checkpoint in between — unlike M6, where chrony/coredns/powerdns were chained without going back to the user.
- Vault stays **a single module** carrying all its features (intermediate PKI, KV, AppRole) — an explicit decision by the user, not split into several modules.
- step-ca drives the **real product** smallstep/step-ca in a container (not a Go re-implementation of `crypto/x509`) — an explicit decision by the user, consistent with the rest of the project (coredns/chrony/powerdns drive real products too).
- Certificates signed in **local/offline** mode (`step certificate sign`, not the network `step ca sign`): entirely avoids the TLS/fingerprint/provisioner bootstrap complexity of step-ca's server mode, which no consumer requires at this milestone.

**Debt**
- No scheduled automatic renewal for the step-ca intermediate or vault's TLS certificates — they are only regenerated/reissued by the next call that finds them expired (or close to expiry for the step-ca intermediate).
- `secrets.kv/v1.List` (vault): an `Unimplemented` stub — KV v2 LIST needs a non-standard HTTP method, and there is no real consumer yet (the migration writes/reads by known ref, never lists).
- The `genesis` AppRole policy is written only once (idempotent through the presence of the stored `role_id`/`secret_id`): if its content has to change later, nothing rewrites it automatically on an already configured vault.
- `file` → `vault` migration proven generically (`test-kv`) but never run end to end with the real `vault` module in a complete `internal/engine.Run()` — explicit scope of M9.
- Update to the M6 note: `vault` has now been built and does not suffer from the session token caching problem identified then (no function subject to repoint is consumed after its own `Check`) — vigilance remains necessary for `openssh-bastion` (M8).

**Next step**: M8 — `teleport` module and seed retirement (doc 07, ADR-017: teleport replaces the OpenSSH bastion initially planned).

### 2026-09-24 — M8 `teleport` module and seed retirement
- `sdk/proto/functions/access/ssh/v1`: new `access.ssh/v1` function (`JumpHost`, `SignUserKey`).
- `modules/teleport`: Teleport Auth+Proxy on its own VM (official APT repository, `stable/v17` channel), proxy TLS certificate through `pki.issuer/v1` + root (`CAChain`) placed in the system trust store (the Proxy validates its own chain at startup). CA pin read from `tctl status`. `fleet.agent/v1.Install`: short-lived enrolment token (`tctl tokens add`) per target, `ssh_service` agent (port 3022, `join_params`). Real `access.ssh/v1.JumpHost`, `SignUserKey` an `Unimplemented` stub (no consumer).
- `Verify`: a disposable target VM enrolled + a user certificate from `tctl auth sign --format=openssh` + a real OpenSSH connection through the agent from a third-party VM.
- Tests (`internal/modulehost/teleport_test.go`): same method as vault — the fake `core.ansible/v1` drives two real Teleport containers (auth+proxy, agent) on a dedicated docker network, with the real certificates/tokens/pins produced by the module; real SSH from the test process. `teleport:14` image: the last one published with a shell (≥ v16 distroless only, no shell → SSH command execution impossible). (Since replaced by a locally built v17 image, #51.)
- Real bugs found by these tests and fixed in the production playbooks: `proxy_listener_mode` belongs to `auth_service` (not `teleport`), enrolment is configured through `join_params` (not `auth_token`), the PKI root must be in the system trust store.
- ADR-019: SSH repoint to the agent deferred (user decision) — native sshd left active, doc 08 criterion adjusted.
- Seed retirement (ADR-020, user decision): the handover no longer shuts the seed down; `internal/engine.retireSeed` stops it at the end of `apply` (reverse plan order) if every seed function has a target successor, if the secrets have migrated to Vault (`secrets` capability), and if a `verify.final` of each target module is green. `state.seed_retired`: a later `apply` no longer queries the seed, no longer replays `Handover`, points directly to the target. `apply` shows what to keep offline. The `genesis seed retire` command is removed.
- Tests: handover → verify.final → seed.retire order, seed never queried again after retirement, seed kept when a function has no successor, CLI output of `apply`.

**Next step**: M9 — Hardening and proof of extensibility (doc 08).
