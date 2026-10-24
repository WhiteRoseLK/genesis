# ADR-111 — AGENTS.md as the single source of AI assistant instructions

- **Status**: accepted
- **Discussion**: [#111](https://github.com/WhiteRoseLK/genesis/issues/111)

Context: the repository is worked on with Claude Code and Antigravity CLI, which read different files. Claude Code reads `CLAUDE.md`, `.claude/rules/` and `.claude/commands/`; Antigravity reads `AGENTS.md`, `.agents/rules/` and `.agents/skills/`. Since ADR-021, the instructions lived in `CLAUDE.md` and `AGENTS.md` only pointed to it, so Antigravity sessions did not reliably load the non-negotiable rules, the path-scoped rules or the commands.

Alternatives: (a) keep the pointer — rejected: one assistant works without the rules; (b) duplicate the content in both files — rejected: two versions drift apart; (d) an Antigravity-specific include in `AGENTS.md` pointing to `CLAUDE.md` — rejected: the vendor-neutral file would depend on a vendor-specific file and syntax.

Decision: `AGENTS.md` holds the project instructions and `CLAUDE.md` only imports it (`@AGENTS.md`). Path-scoped rules live in `.agents/rules/*.md` with both frontmatters: `paths:` for Claude Code, and `trigger: model_decision` with a `description` naming the directories for Antigravity. `trigger: glob` was tested first and is not applied by Antigravity CLI (neither on read nor on edit), whereas `model_decision` rules are loaded by the agent when it works in the described directories; `.claude/rules` is a symlink to `.agents/rules`. Commands are skills in `.agents/skills/<name>/SKILL.md`, linked from `.claude/skills/<name>`. A new rule or skill is added once, under `.agents/`.

Consequences: this supersedes the "`AGENTS.md` points to `CLAUDE.md`" part of ADR-021; the rest of ADR-021 is unchanged. `CONTRIBUTING.md`, the PR template, the `Makefile` and `.golangci.yml` now reference `AGENTS.md` and `.agents/rules/`; past ADRs keep their wording. The `/milestone` and `/check-architecture` commands keep their names as skills. Contributors on Windows need Git symlink support (`core.symlinks`) for the `.claude/` links.
