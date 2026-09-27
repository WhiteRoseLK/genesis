# Contributing to Genesis

The architecture rules and the non-negotiable rules are in [`CLAUDE.md`](CLAUDE.md) and [`docs/`](docs/): this document only describes the development process (ADR-021).

By participating, you agree to abide by the [code of conduct](CODE_OF_CONDUCT.md).

## Environment

- Go (the version in `go.mod`), `make`, Docker or Podman (integration tests). The other tools (`golangci-lint`, `buf`, protobuf generators, `govulncheck`, `go-licenses`) are installed at pinned versions by `make tools`, into `.bin/`.
- Monorepo: each module has its own `go.mod`, complete and usable outside the repository (ADR-022). The `go.work` workspace is for everyday development. Go through the `Makefile`, which walks every `go.mod`.

| Command | Role |
|---|---|
| `make tools` | Installs the pinned development tools into `.bin/` |
| `make build` | Builds every module (`CGO_ENABLED=0`, `GOARCH=amd64` or `arm64`) |
| `make test` | Unit tests of each `go.mod`: no network, no container daemon |
| `make test-race` | Same tests with the race detector (CGO required for the tests only) |
| `make test-docker` | Integration tests against real containers (`docker` build tag) |
| `make pull-images` | Pulls the images pinned in the code ahead of time, with retries (registry rate limits) |
| `make lint` | `golangci-lint`, including the `depguard` import rules |
| `make mod-check` | Each `go.mod` is up to date and builds outside `go.work` |
| `make proto` | Regenerates the protobuf code (`buf generate`): the generated code is committed |
| `make proto-check` | `buf lint` and generated code up to date (CI also runs `buf breaking` against the base branch) |
| `make vuln` | Known reachable vulnerabilities (`govulncheck`) |
| `make licenses` | Dependency licenses compatible with Apache-2.0 |
| `make e2e` | End to end on Proxmox: **never without an explicit request** |

## Issues

- Every topic starts from an issue: **Bug**, **Feature** or **Technical debt** (templates in `.github/ISSUE_TEMPLATE/`).
- A new issue gets `needs-triage`, removed automatically once a `priority:high|medium|low` label is set.
- **Milestones** (ADR-049): each milestone of `docs/08-milestones.md` has a [GitHub milestone](https://github.com/WhiteRoseLK/genesis/milestones) and a parent issue (label `milestone`); each planned PR is a sub-issue, attached to the GitHub milestone. The [GitHub Project](https://github.com/users/WhiteRoseLK/projects) gives the board and roadmap views.
- **Decisions**: an architecture or process choice is discussed in a **"Decision"** issue (label `decision`). Once settled, the PR that applies it adds `docs/adr/NNNN-title.md` (NNNN = the issue number, template `docs/adr/_template.md`) and its row in `docs/09-decisions.md`, then closes the issue.
- **Debt**: a deliberately deferred limitation gets `tech-debt`; the issue is the single source of truth for debt.

## Branches and atomic PRs

- `main` is protected: nobody pushes to it directly, and the CI checks are required.
- **One PR = one topic.** A milestone is split into several successive PRs, each green and reviewable on its own.
- Naming: `<type>/<short-topic>`, e.g. `feat/teleport-agent`, `fix/vault-token-cache`, `docs/adr-021`.
- The PR follows the `.github/PULL_REQUEST_TEMPLATE.md` template and links its issue (`Closes #N`).
- **Squash** merges only, branch deleted after merge.

## Conventional Commits

The PR title becomes the commit on `main`: CI validates it and it feeds the CHANGELOG.

```text
<type>(<scope>): <short description>
```

- **Types**: `feat`, `fix`, `perf`, `refactor`, `revert`, `test`, `docs`, `ci`, `build`, `chore`.
- **Scope** (optional, free-form): core layer (`engine`, `broker`, `resolver`, `spec`…), `sdk`, `proto`, or the module name (`teleport`, `vault`…). CI keeps no list: adding a module changes nothing outside its directory.
- Breaking change (module contract, spec or state format, SDK import paths): `!` after the scope or `BREAKING CHANGE:` in the body.

## Documentation to keep up to date in the same PR

- `docs/adr/` and the `docs/09-decisions.md` index: an ADR for any structural decision or new heavy dependency, coming from its "Decision" issue.
- The relevant design documents (`docs/0x-*.md`) if the contract, the spec or the lifecycle changes; `docs/10-adding-a-module.md` if the procedure for adding a module changes.
- `docs/PROGRESS.md` is only updated at the end of a milestone; the milestone summary goes in the closing comment of its parent issue. The details of each change live in its PR description and in the CHANGELOG.

## Security

Never report a vulnerability in a public issue: see [`SECURITY.md`](SECURITY.md).

## Releases

[Release Please](https://github.com/googleapis/release-please) keeps one release PR per package up to date:

- **core**: `vX.Y.Z` tag, `CHANGELOG.md` at the root, version written to `internal/version` (compared with the modules' `core` constraint);
- **SDK**: `sdk/vX.Y.Z` tag (Go submodule convention), `sdk/CHANGELOG.md`.

Before 1.0, a `feat` bumps the patch version and a breaking change bumps the minor version. A minor release changes the version compared with the modules' `core` constraint: **in the PR that introduces a breaking change**, raise the upper bound of the constraint of the repository's modules (`modules/*/module.yaml` and the `scaffold` template) if the change does not stop them from working with the new core; otherwise the release PR fails in CI. Test modules (`test/modules/*`) have no upper bound. Binary publishing (GoReleaser) will come later.

## Dependencies

Dependabot proposes updates to the GitHub Actions and to every `go.mod` each week: one PR per dependency, updating it in every module at once. Container images are pinned by version and digest in the code: updating them is manual for now.
