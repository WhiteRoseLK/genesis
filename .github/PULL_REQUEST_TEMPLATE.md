<!--
One PR = one topic (ADR-021). Squash merge: the title becomes the commit on
main and follows Conventional Commits, e.g. `feat(engine): …`.
-->

## Description

<!-- What the PR changes and why. -->

Closes #

## Checklist

- [ ] Tests added or updated (`make test`, plus `make test-docker` when containers are involved); SDK conformance suite green for every module touched.
- [ ] Non-negotiable rules of `CLAUDE.md` respected: no plaintext secret, idempotence (`Check` before any action), no forbidden import, no change outside the directory of an added module.
- [ ] ADR (`docs/adr/`, linked "Decision" issue) and design documents updated if a decision, the contract, the spec or the lifecycle changes.
- [ ] New debt → `tech-debt` issue.
