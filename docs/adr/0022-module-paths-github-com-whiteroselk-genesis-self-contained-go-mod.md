# ADR-022 — Module paths `github.com/WhiteRoseLK/genesis`, self-contained go.mod files

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Context: since M0, the Go modules were called `genesis`, `genesis/sdk` and `genesis-module-<name>`, with no domain, "as long as the GitHub repository does not exist". Consequence discovered in M3: the modules' `go.mod` files declared no dependency (not even the SDK) and only compiled thanks to `go.work`. Neither `go install`, nor a module written outside the repository, nor Dependabot could work. The repository now exists and is public.

Alternatives: (a) keep the domain-less paths — rejected: the SDK is the project's public API and must be importable by a third-party module; (b) publish the SDK as a tagged version right away (`sdk/v0.x`) and use it in the `require` directives, without `replace` — deferred: the SDK would have to be re-tagged on every contract change during iteration 1.

Decision: paths `github.com/WhiteRoseLK/genesis` (core), `.../sdk`, `.../modules/<name>` and `.../test/modules/<name>`. Each `go.mod` declares all its dependencies (`go mod tidy`) and references the repository's SDK with `require .../sdk v0.0.0-…` + `replace => <relative path>`. `go.work` remains the everyday development tool. CI checks that each `go.mod` is up to date and compiles outside the workspace (`make mod-check`). `genesis modules scaffold` generates a complete `go.mod` and runs `go mod tidy`.

Consequence: the earlier M3 decision ("no `require genesis/sdk`, no `go mod tidy`") is superseded. The `replace` directives still prevent `go install …@version`; they will disappear once the SDK is published as a tagged version.
