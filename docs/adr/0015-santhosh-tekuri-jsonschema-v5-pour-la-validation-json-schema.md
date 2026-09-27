# ADR-015 — `santhosh-tekuri/jsonschema/v5` pour la validation JSON Schema

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Contexte : le doc 04 exige une validation structurelle de la spec par JSON Schema (J1), et le doc 03 prévoit qu'un module publie son `config_schema` en JSON Schema (J3) — les deux ont besoin du même mécanisme de validation. Alternatives : validation Go écrite à la main (pas de dépendance, mais logique dupliquée entre l'enveloppe générale et chaque `config_schema` de module, et divergence probable des messages d'erreur) ; `xeipuuv/gojsonschema` (moins maintenu, draft-4 uniquement). Décision : `santhosh-tekuri/jsonschema/v5`, pur Go, support draft 2020-12, pas de cgo. Conséquence : dépendance ajoutée au module racine (`internal/spec`) dès J1, réutilisée telle quelle par le cœur en J3 pour valider les `config_schema` des modules.
