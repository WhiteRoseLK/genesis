# Domain Docs

How the engineering skills should consume Genesis's domain documentation when exploring the codebase. Genesis is a single-context repo.

## Before exploring, read these

- **Vocabulary**: the `## Vocabulary` table in `docs/02-architecture.md`. It plays the role of `GLOSSARY.md`; do not create a separate `GLOSSARY.md`.
- **Decisions**: `docs/09-decisions.md`, the index of every ADR, then the ADRs in `docs/adr/` that touch the area you're about to work in. ADRs 001 to 022 predate the one-file-per-ADR process: some exist only as a row in the index.
- **Design documents**: the `docs/0X-*.md` file relevant to the task, as listed in `AGENTS.md`.

## Use the project's vocabulary

When your output names a domain concept (in an issue title, a refactor proposal, a hypothesis, a test name), use the term as defined in the Vocabulary table. Don't drift to synonyms.

If the concept you need isn't in the table yet, that's a signal: either you're inventing language the project doesn't use (reconsider) or there's a real gap. Add the term to the Vocabulary table of `docs/02-architecture.md`, in the same PR as the change that needs it.

## Recording a decision

Follow ADR-049, not a free-form ADR: open an issue from the "Decision" template (label `decision`), let the user decide on the issue, then the PR that applies it copies `docs/adr/_template.md` to `docs/adr/NNNN-title.md` (NNNN = the issue number) and adds its row to `docs/09-decisions.md`.

## Flag ADR conflicts

If your output contradicts an existing ADR, surface it explicitly rather than silently overriding:

> _Contradicts ADR-0012 (modules as separate processes), but worth reopening because…_
