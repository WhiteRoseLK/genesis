# ADR-049 — Milestones tracked on GitHub, ADRs as files numbered by their issue

- **Status**: accepted
- **Discussion**: [#49](https://github.com/WhiteRoseLK/genesis/issues/49)

Context: milestone tracking relied on `docs/PROGRESS.md`, `docs/08-milestones.md` and an unstructured milestone issue. Decisions lived in a single file, `docs/09-decisions.md`, numbered by hand: an ADR-021/022 collision happened between two PRs opened in parallel, conflicts there were frequent, and the discussion leading to a decision was kept nowhere.

Alternatives: (a) keep everything in the repository — rejected: conflicts, manual numbering, lost discussions; (b) move everything to GitHub (issues or Discussions) — rejected for decisions: they would leave the code, would no longer go through PR review, and an assistant without access to the GitHub API could no longer read them.

Decision:
- **Milestones**: one GitHub milestone per project milestone; a parent issue (label `milestone`) whose sub-issues each match one atomic PR; a GitHub Project (board and roadmap) for the overview. `docs/08-milestones.md` remains the definition of the milestones and their acceptance criteria; `docs/PROGRESS.md` is limited to a summary pointing to the current milestone.
- **Decisions**: the discussion takes place in an issue created from the "Decision" template (label `decision`); the user decides in a comment; the PR that applies the decision adds `docs/adr/NNNN-title.md`, where NNNN is the issue number, and closes the issue (`Closes #NNNN`). Since issue numbers are unique, no collision is possible. A superseded ADR is not deleted: its status becomes "superseded by ADR-NNN".
- ADRs 001 to 022 are split once into `docs/adr/0001…0022`, without changing their content; `docs/09-decisions.md` becomes the index.

Consequences: `/milestone`, `CLAUDE.md` and `CONTRIBUTING.md` follow this process. The GitHub Project is created from the web interface (the account's GraphQL API is outside the assistant's tools).
