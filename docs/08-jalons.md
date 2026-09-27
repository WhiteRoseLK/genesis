# 08 — Jalons et critères d'acceptation

> Ce document définit chaque jalon et ses critères d'acceptation. Leur **suivi** se fait sur GitHub (ADR-049) : un [milestone](https://github.com/WhiteRoseLK/genesis/milestones) par jalon, une issue parente `jalon` et une sous-issue par PR.

Implémenter dans l'ordre. Chaque jalon est livrable et testable seul.

## J0 — Squelette
- Monorepo : `cmd/genesis`, `internal/`, `sdk/` (go.mod propre), `modules/` ; `LICENSE` Apache 2.0, `NOTICE`, en-têtes SPDX ; `Makefile` (`build`, `test`, `lint`, `proto`) ; `buf` pour le protobuf.
- CI : lint, tests, vérification des licences des dépendances, **règles d'import** (cœur ↛ modules, modules ↛ cœur).
- **Accepté si** : `genesis --help` liste les commandes du doc 02 ; un import interdit fait échouer la CI.

## J1 — Spec et validation générale
- Types, défauts, validation structurelle, JSON Schema, refus des secrets littéraux.
- **Accepté si** : exemple du doc 04 valide ; 10 specs invalides échouent avec chemin YAML.

## J2 — Secrets et état
- `init`, clé maîtresse, backend `file` age, redaction, état avec verrou.
- **Accepté si** : `Ensure` idempotent ; deux `apply` concurrents → le second refuse ; test de redaction vert.

## J3 — SDK et hôte de modules
- Protocole `module/v1`, `sdk.Serve`, hôte `go-plugin`, découverte, `genesis.lock` avec empreintes, `genesis modules list/install/verify`, `genesis modules scaffold`.
- Harnais de test et suite de conformité (squelette).
- **Accepté si** : un module généré par `scaffold` compile, est découvert, répond à `Describe` ; un module dont l'empreinte diffère du lock est refusé ; un module qui plante pendant une étape produit une erreur propre sans arrêter le cœur.

## J4 — Résolveur, broker, planificateur, moteur
- Résolution capacités → modules (défauts, ajouts automatiques, conflits), broker avec contrôle d'accès, fonctions `core.*`, DAG, `plan`, moteur avec reprise et audit.
- Modules de test `test-a → test-b → test-c` avec phase graine et passation, et module `fake-compute`.
- **Accepté si** : ordre correct ; cycle détecté ; appel d'une fonction non déclarée refusé par le broker ; kill puis relance → reprise ; second `apply` → 0 changement ; **ajout d'un module `test-d` dépendant de `test-a` sans modifier aucun fichier hors `test/modules/test-d/`** → inséré au bon endroit dans le plan.

## J5 — Modules `proxmox` et `base-os`
- **Accepté si** : spec `compute` + une VM placée → VM créée, joignable, durcie ; relance → 0 changement ; `destroy` la supprime ; conformité SDK verte.

## J6 — Modules `chrony`, `coredns`, `powerdns`
- **Accepté si** : `Verify` du doc 07 ; après passation, arrêt de `coredns` sans impact.

## J7 — Modules `step-ca`, `vault`
- **Accepté si** : `Verify` du doc 07 ; secrets non-`recovery` migrés vers Vault.

## J8 — Module `teleport` et retrait de la graine
- **Accepté si** : connexion via l'agent Teleport OK ; après retrait automatique de la graine en fin d'`apply` (ADR-020), environnement vert.
- **Repoint différé (dette, doc07)** : le refus de l'accès SSH direct n'est pas exigé à ce jalon — `fleet.agent/v1.Install` laisse le sshd natif actif, `core.ansible/v1` continue d'utiliser l'accès direct par clé. Faire basculer le cœur vers l'agent Teleport est tracé par une issue dédiée, hors périmètre J8.

## J9 — Durcissement et preuve d'extensibilité
- Test e2e complet sur Proxmox (build tag `integration`), documentation utilisateur et guide module (doc 10).
- Preuve : module `bind` minimal remplaçant `powerdns`, e2e vert sans modification du cœur ni des autres modules.
- **Accepté si** : critère de succès global du doc 01 (< 30 min, idempotent) avec `powerdns` **et** avec `bind`.

## Itération 2 (hors périmètre)
Air-gap (bundle d'artefacts et modules), GitLab, supervision, sauvegarde, module Nutanix, HA Vault, day-2, signature et registre de modules, racine de confiance matérielle, puis couche physique (`pxe-seed`, `redfish`, `proxmox-install`).
