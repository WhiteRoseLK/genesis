<!--
Une PR = un sujet (ADR-021). Fusion en squash : le titre devient le commit
sur main et suit Conventional Commits, ex. `feat(engine): …`.
-->

## Description

<!-- Ce que change la PR et pourquoi. -->

Closes #

## Checklist

- [ ] Tests ajoutés ou mis à jour (`make test`, `make test-docker` si des conteneurs sont concernés) ; suite de conformité SDK verte pour tout module touché.
- [ ] Règles non négociables de `CLAUDE.md` respectées : aucun secret en clair, idempotence (`Check` avant toute action), aucun import interdit, aucune modification hors du répertoire d'un module ajouté.
- [ ] ADR (`docs/adr/`, issue « Décision » liée) et documents de conception à jour si une décision, le contrat, la spec ou le cycle changent.
- [ ] Dette nouvelle → issue `dette-technique`.
