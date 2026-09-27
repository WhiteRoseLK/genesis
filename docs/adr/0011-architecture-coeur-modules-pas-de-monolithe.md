# ADR-011 — Architecture cœur + modules, pas de monolithe

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Contexte : ajouter des produits et, plus tard, la couche physique sans refonte. Décision : cœur générique ; un module par produit ; interactions uniquement via des fonctions versionnées routées par un broker ; ordre de construction dérivé des dépendances, aucune couche codée en dur. Conséquences : SDK à maintenir, discipline de versionnement des API de fonctions, suite de conformité obligatoire.
