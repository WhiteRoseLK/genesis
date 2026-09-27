# ADR-054 — English as the project language

- **Status**: accepted
- **Discussion**: [#54](https://github.com/WhiteRoseLK/genesis/issues/54)

Context: Genesis was written in French — design documents, ADRs, README, code comments, error messages, CLI output, issues and PR titles. Now that the repository is public, French limits its reach to international users and contributors.

Alternatives: (a) keep French — rejected: the project stays hard to discover and to contribute to; (b) bilingual documentation — rejected: every document would be maintained twice and the versions would drift apart.

Decision: everything published in the repository is in English: documentation, ADRs, code comments, error messages, CLI output and help, Ansible task names, GitHub templates, labels, milestones, issues and PR titles. Milestones are renamed `J0`–`J9` → `M0`–`M9`, the `/jalon` command becomes `/milestone`, and the `dette-technique`, `jalon` and `priorite:*` labels become `tech-debt`, `milestone` and `priority:*`. File names of design documents and ADRs are translated, keeping their numbers. Conversations with the maintainer may stay in French.

Consequences: a one-off translation, applied in three PRs (documentation and repository metadata, core code, modules and tests). Past CHANGELOG entries stay as generated, because they are release history. The historical milestone journal (`docs/journal.md`) is translated as well.
