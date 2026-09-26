<!--
Une PR = un sujet (ADR-021). Elle est fusionnée en squash : son titre devient
le commit sur main et doit suivre Conventional Commits, ex. `feat(engine): …`.
-->

## Description

<!-- Ce que change la PR et pourquoi, en quelques lignes. -->

## Issue / jalon liés

<!-- Closes #123 — et le jalon concerné (docs/08-jalons.md), ex. J9. -->
Closes #

## Type de changement

- [ ] 🐛 Correction (`fix`)
- [ ] ✨ Fonctionnalité (`feat`)
- [ ] 💥 Changement cassant (`!` dans le titre ou `BREAKING CHANGE:`) — contrat module, format de spec ou d'état
- [ ] 📝 Documentation (`docs`)
- [ ] 🧹 Refactorisation, tests, outillage (`refactor`, `test`, `ci`, `chore`)

## Checklist

**Qualité**
- [ ] `make lint test` vert.
- [ ] Tests ajoutés ou mis à jour, sans réseau (`fake-compute`, conteneurs jetables) ; suite de conformité SDK verte pour tout module touché.
- [ ] `make e2e` **non lancé**, sauf demande explicite (nécessite un Proxmox).

**Règles non négociables (CLAUDE.md)**
- [ ] 🔒 Aucun secret en clair dans les logs, sorties, état, erreurs, fixtures ou commits (type `Secret`, `secrets.Ref`).
- [ ] ♻️ Idempotence : `Check` avant toute action ; une relance ne produit aucun changement.
- [ ] 🧱 `internal/` n'importe ni `modules/` ni une bibliothèque propre à un produit ; aucun `switch` sur un nom de produit ni ordre de couche codé en dur.
- [ ] 🧩 `modules/*` n'importe que `sdk/` et des bibliothèques tierces ; les interactions passent par le broker et les `requires`.
- [ ] ➕ Ajouter un module ne nécessite aucune modification hors de son répertoire.
- [ ] 🧭 Erreurs actionnables (module, étape, cause, piste).

**Documentation**
- [ ] `docs/PROGRESS.md` à jour (fait, décisions, dette, prochaine étape).
- [ ] Décision structurante ou nouvelle dépendance lourde → ADR dans `docs/09-decisions.md`.
- [ ] Documents de conception (`docs/0x-*.md`) mis à jour si le contrat, la spec ou le cycle changent.
- [ ] Dette nouvelle → issue avec le label `dette-technique`.
