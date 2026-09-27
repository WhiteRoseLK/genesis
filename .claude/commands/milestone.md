---
description: Start or resume the implementation of a Genesis milestone
argument-hint: <milestone number, e.g. M9>
---
Requested milestone: $ARGUMENTS

Milestones are tracked on GitHub (ADR-049): one GitHub milestone per project milestone, a parent issue (label `milestone`) and one sub-issue per atomic PR.

1. Read `docs/PROGRESS.md`, the $ARGUMENTS section of `docs/08-milestones.md`, then its parent issue and its sub-issues on GitHub (GitHub milestone with the same name).
2. Check that the previous milestones are finished (GitHub milestones closed or marked "done"); otherwise, report it and stop.
3. Read only the design documents and ADRs (`docs/adr/`) this milestone needs.
4. Propose a plan: a split into **atomic PRs** (one topic each, ADR-021), with for each one the Conventional Commits title, the files, interfaces and tests, and how it maps to each acceptance criterion. Flag any ambiguity. **Wait for my approval before coding.**
5. After approval: create or update one sub-issue per planned PR, attached to the parent issue and the GitHub milestone. Any structural decision to be made becomes a "Decision" issue (label `decision`): do not decide alone.
6. For each PR: a `<type>/<topic>` branch from an up-to-date `main`, `make lint test` green, PR opened with the template and `Closes #<sub-issue>`. A settled decision is added as `docs/adr/NNNN-title.md` in the PR that applies it (`Closes #NNNN`).
7. At the end of the milestone: check each acceptance criterion one by one and report in a closing comment on the parent issue (what was done, decisions with their ADRs, debt with issue numbers, measurements); update the table in `docs/PROGRESS.md`; close the GitHub milestone. Any new debt becomes a `tech-debt` issue.
