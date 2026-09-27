# ADR-013 — Monorepo, separate artefacts

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Core, SDK and official modules live in one repository to iterate fast, but each has its own `go.mod` and its own binary. Modules can move to dedicated repositories without any change to the contract.
