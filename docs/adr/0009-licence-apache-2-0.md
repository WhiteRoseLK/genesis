# ADR-009 — Licence Apache 2.0

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Licence permissive : utilisation, modification et intégration commerciale autorisées, y compris par des intégrateurs, avec obligation de conserver la notice et clause de brevets protégeant contributeurs et utilisateurs. Alternative écartée : AGPL-3.0, qui protège contre une reprise en SaaS fermé mais freine l'adoption en entreprise. Conséquences : fichier `LICENSE` Apache 2.0 et fichier `NOTICE` à la racine, en-tête SPDX `// SPDX-License-Identifier: Apache-2.0` dans chaque fichier source, dépendances à licence compatible uniquement (pas de GPL/AGPL dans le binaire).
