# ADR-049 — Jalons suivis sur GitHub, ADR en fichiers numérotés par leur issue

- **Statut** : acceptée
- **Débat** : [#49](https://github.com/WhiteRoseLK/genesis/issues/49)

Contexte : le suivi des jalons reposait sur `docs/PROGRESS.md`, `docs/08-jalons.md` et une issue `jalon` sans structure. Les décisions tenaient dans un seul fichier, `docs/09-decisions.md`, numérotées à la main : une collision ADR-021/022 est survenue entre deux PR ouvertes en parallèle, les conflits y étaient fréquents, et le débat qui mène à une décision n'était conservé nulle part.

Alternatives : (a) tout garder dans le dépôt — rejetée : conflits, numérotation manuelle, débat perdu ; (b) tout passer sur GitHub (issues ou Discussions) — rejetée pour les décisions : elles quitteraient le code, ne passeraient plus en revue de PR, et un assistant sans accès à l'API GitHub ne les lirait plus.

Décision :
- **Jalons** : un milestone GitHub par jalon ; une issue parente (label `jalon`) dont chaque sous-issue correspond à une PR atomique ; un GitHub Project (tableau et roadmap) pour la vue d'ensemble. `docs/08-jalons.md` reste la définition des jalons et de leurs critères d'acceptation ; `docs/PROGRESS.md` se limite à un résumé qui renvoie au milestone en cours.
- **Décisions** : le débat se tient dans une issue créée avec le modèle « Décision » (label `decision`) ; l'utilisateur tranche en commentaire ; la PR qui applique la décision ajoute `docs/adr/NNNN-titre.md`, où NNNN est le numéro de l'issue, et ferme l'issue (`Closes #NNNN`). Les numéros d'issue étant uniques, aucune collision n'est possible. Une ADR remplacée n'est pas supprimée : son statut devient « remplacée par ADR-NNN ».
- Les ADR 001 à 022 sont découpées une fois en `docs/adr/0001…0022`, sans modification de leur contenu ; `docs/09-decisions.md` devient l'index.

Conséquences : `/jalon`, `CLAUDE.md` et `CONTRIBUTING.md` suivent ce processus. Le GitHub Project se crée depuis l'interface (API GraphQL du compte, hors des outils de l'assistant).
