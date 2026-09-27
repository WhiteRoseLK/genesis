---
description: Démarrer ou reprendre l'implémentation d'un jalon Genesis
argument-hint: <numéro de jalon, ex. J9>
---
Jalon demandé : $ARGUMENTS

Le suivi des jalons vit sur GitHub (ADR-049) : un milestone par jalon, une issue parente (label `jalon`) et une sous-issue par PR atomique.

1. Lis `docs/PROGRESS.md`, la section du jalon $ARGUMENTS dans `docs/08-jalons.md`, puis son issue parente et ses sous-issues sur GitHub (milestone du même nom).
2. Vérifie que les jalons précédents sont terminés (milestones fermés ou marqués « fait ») ; sinon, signale-le et arrête-toi.
3. Lis uniquement les documents de conception et les ADR (`docs/adr/`) nécessaires à ce jalon.
4. Propose un plan : découpage en **PR atomiques** (un sujet chacune, ADR-021), avec pour chacune le titre Conventional Commits, les fichiers, interfaces et tests, et la correspondance avec chaque critère d'acceptation. Signale toute ambiguïté. **Attends ma validation avant de coder.**
5. Après validation : crée ou mets à jour une sous-issue par PR prévue, rattachée à l'issue parente et au milestone. Toute décision structurante à prendre devient une issue « Décision » (label `decision`) : ne tranche pas seul.
6. Pour chaque PR : une branche `<type>/<sujet>` depuis `main` à jour, `make lint test` verts, PR ouverte avec le modèle et `Closes #<sous-issue>`. Une décision tranchée s'ajoute en `docs/adr/NNNN-titre.md` dans la PR qui l'applique (`Closes #NNNN`).
7. En fin de jalon : vérifie chaque critère d'acceptation un par un et rends compte en commentaire de clôture de l'issue parente (fait, décisions avec leurs ADR, dette avec numéros d'issue, mesures) ; mets à jour le tableau de `docs/PROGRESS.md` ; ferme le milestone. Toute dette nouvelle devient une issue `dette-technique`.
