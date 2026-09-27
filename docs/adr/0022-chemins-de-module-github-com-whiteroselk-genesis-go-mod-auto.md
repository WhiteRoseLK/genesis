# ADR-022 — Chemins de module `github.com/WhiteRoseLK/genesis`, go.mod autonomes

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Contexte : depuis J0, les modules Go s'appelaient `genesis`, `genesis/sdk` et `genesis-module-<nom>`, sans domaine, « tant que le dépôt GitHub n'existe pas ». Conséquence découverte en J3 : les `go.mod` des modules ne déclaraient aucune dépendance (même pas le SDK) et ne compilaient que grâce à `go.work`. Ni `go install`, ni un module écrit hors du dépôt, ni Dependabot ne pouvaient fonctionner. Le dépôt existe désormais et il est public.

Alternatives : (a) garder les chemins sans domaine — rejetée : le SDK est l'API publique du projet et doit être importable par un module tiers ; (b) publier tout de suite le SDK en version taguée (`sdk/v0.x`) et s'en servir dans les `require`, sans `replace` — différée : il faudrait retaguer le SDK à chaque modification du contrat pendant l'itération 1.

Décision : chemins `github.com/WhiteRoseLK/genesis` (cœur), `.../sdk`, `.../modules/<nom>` et `.../test/modules/<nom>`. Chaque `go.mod` déclare toutes ses dépendances (`go mod tidy`) et référence le SDK du dépôt par `require .../sdk v0.0.0-…` + `replace => <chemin relatif>`. `go.work` reste l'outil du développement quotidien. La CI vérifie que chaque `go.mod` est à jour et se compile hors de l'espace de travail (`make mod-check`). `genesis modules scaffold` génère un `go.mod` complet et lance `go mod tidy`.

Conséquence : l'ancienne décision de J3 (« pas de `require genesis/sdk`, pas de `go mod tidy` ») est remplacée. Les `replace` empêchent encore `go install …@version` ; ils disparaîtront quand le SDK sera publié en version taguée.
