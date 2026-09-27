# Genesis

Open-source tool (Go, Apache 2.0) that automatically builds a self-sufficient environment foundation (NTP, DNS, PKI, Vault, bastion…) on a virtualisation cluster, from a YAML spec. **Generic core + plugin modules** architecture (HashiCorp go-plugin, gRPC).

## Design documentation (source of truth)
Do not load everything up front: read the document relevant to the task at hand.
- `docs/01-vision-scope.md` — scope and non-goals of iteration 1
- `docs/02-architecture.md` — core, modules, functions, broker, repository layout
- `docs/03-module-contract.md` — manifest, gRPC protocol, functions, rules
- `docs/04-spec.md` — user spec format
- `docs/05-bootstrap-lifecycle.md` — seed → target → handover → retirement
- `docs/06-secrets-state.md` — secrets, redaction, state
- `docs/07-mvp-modules.md` — iteration 1 modules
- `docs/08-milestones.md` — milestones and acceptance criteria
- `docs/09-decisions.md` — ADR index; one ADR per file in `docs/adr/`
- `docs/10-adding-a-module.md` — procedure for adding a module
- `docs/PROGRESS.md` — **progress: read at the start of each session** (milestone status, next step); updated at the end of a milestone
- Tracking of the current milestone: GitHub milestone, parent issue labelled `milestone` and its sub-issues (one per PR) — ADR-049
- `docs/journal.md` — detailed history of milestones M0 to M8 (consult when needed); after that, the closing comment of the milestone's parent issue
- Technical debt: GitHub `tech-debt` issues (single source of truth)

## Way of working
- Everything published in the repository is in English (ADR-054): code, comments, messages, documentation, commits, PRs, issues.
- Commits, PRs, issues and comments carry no AI-tool attribution: no "Generated with…" footer, no `Co-Authored-By` trailer, no session link.
- Implement **one milestone at a time**, in the order of doc 08. Do not anticipate later milestones.
- Before coding a milestone: propose a short plan (files, interfaces, tests) and wait for approval.
- If a document is ambiguous or contradictory: ask rather than guess. A structural decision is discussed in a "Decision" issue (label `decision`), then added as `docs/adr/NNNN-title.md` (NNNN = the issue number) in the PR that applies it, with its row in `docs/09-decisions.md` (ADR-049).
- **One atomic PR per topic**, squash-merged (ADR-021): a milestone = several successive PRs. Branch `<type>/<topic>` from `main`, PR title in Conventional Commits (optional scope: core layer, `sdk`, `proto` or module name), PR template filled in, `Closes #N`. Details in `CONTRIBUTING.md`.
- Every PR: `make lint test` green, ADRs and design documents updated in the same PR. New debt → `tech-debt` issue.
- A milestone = a GitHub milestone, a parent issue labelled `milestone`, one sub-issue per PR (ADR-049). End of milestone: acceptance criteria checked one by one, report in the closing comment of the parent issue, table in `docs/PROGRESS.md` updated, GitHub milestone closed.
- Never merge a PR without the user's approval.

## Non-negotiable rules
- **No plaintext secret** in logs, outputs, state, errors, fixtures or commits. `Secret` type with redaction.
- **Idempotence**: `Check` before any action; re-running produces no change.
- `internal/` never imports `modules/` or a product-specific library. No `switch` on a product name, no hard-coded layer order.
- `modules/*` imports only `sdk/` + third-party libraries. Modules interact only through the broker and the functions declared in `requires`.
- Adding a module must not require any change outside its directory.
- Plan before apply; actionable errors (module, step, cause, hint).
- New heavy dependency → ADR.

## Stack and commands
- Stable Go, `CGO_ENABLED=0`, linux/amd64 and linux/arm64 targets. `cobra`, `yaml.v3`, `log/slog`, `buf` for protobuf, `hashicorp/go-plugin`, `filippo.io/age`.
- `make tools` (pinned tools in `.bin/`) · `make build` · `make test` · `make test-race` · `make test-docker` · `make lint` · `make mod-check` · `make proto` / `make proto-check` · `make vuln` · `make licenses` · `make e2e` (`integration` build tag, needs a Proxmox: never run without an explicit request).
- Unit tests without network or container daemon (`make test`); tests against real containers under the `docker` build tag (`make test-docker`), images pinned by version and digest; tests of a module through the core in `test/integration/`; `fake-compute` module for end-to-end tests; SDK conformance suite mandatory for every module.

## Known pitfalls
- Never test against a real Proxmox or run `genesis apply`/`destroy` on real infrastructure without an explicit request.
- Each module has its own `go.mod`: run Go commands from the right directory or through the `Makefile` (`go.work` workspace).
