---
description: Démarrer ou reprendre l'implémentation d'un jalon Genesis
argument-hint: <numéro de jalon, ex. J3>
---
Jalon demandé : $ARGUMENTS

1. Lis `docs/PROGRESS.md` puis la section du jalon $ARGUMENTS dans `docs/08-jalons.md`.
2. Vérifie que les jalons précédents sont marqués terminés ; sinon, signale-le et arrête-toi.
3. Lis uniquement les documents de conception nécessaires à ce jalon.
4. Propose un plan : fichiers à créer ou modifier, interfaces, tests, correspondance avec chaque critère d'acceptation. Signale toute ambiguïté. **Attends ma validation avant de coder.**
5. Dans le plan, découpe le jalon en **PR atomiques** (un sujet chacune, ADR-021) et donne le titre Conventional Commits de chacune.
6. Après validation : une branche `<type>/<sujet>` par PR depuis `main` à jour, tests et `make lint test` verts, PR ouverte avec le modèle et `Closes #N`. Attends ma validation avant toute fusion (squash).
7. En fin de jalon : vérifie chaque critère d'acceptation un par un, lance `make lint test`, mets à jour `docs/PROGRESS.md` (état, décisions, prochaine étape) ; toute dette nouvelle devient une issue `dette-technique`.
