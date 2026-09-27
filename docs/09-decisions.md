# 09 — Registre des décisions (ADR)

Chaque décision structurante est un fichier de [`docs/adr/`](adr/) : contexte → alternatives → décision → conséquences (ADR-049).

**Nouvelle décision** :
1. Ouvrir une issue avec le modèle **« Décision »** (label `decision`) : contexte, options, recommandation.
2. L'utilisateur tranche en commentaire de l'issue.
3. La PR qui applique la décision copie [`adr/_modele.md`](adr/_modele.md) en `adr/NNNN-titre.md` (NNNN = numéro de l'issue), ajoute la ligne ci-dessous et ferme l'issue (`Closes #NNNN`).

Une décision remplacée n'est pas supprimée : son statut devient « remplacée par ADR-NNN ». Les ADR 001 à 022 précèdent ce processus et gardent leur numéro historique.

| ADR | Décision | Statut |
|---|---|---|
| [ADR-001](adr/0001-go-binaire-unique.md) | Go, binaire unique | acceptée |
| [ADR-002](adr/0002-orchestrer-ansible-plutot-que-recoder-la-configuration.md) | Orchestrer Ansible plutôt que recoder la configuration | acceptée |
| [ADR-003](adr/0003-pas-de-terraform-opentofu-dans-literation-1.md) | Pas de Terraform/OpenTofu dans l'itération 1 | acceptée |
| [ADR-004](adr/0004-proxmox-comme-premier-module-compute.md) | Proxmox comme premier module compute | acceptée |
| [ADR-005](adr/0005-capacites-decrites-par-fonctions-fournies-graine-cible.md) | Capacités décrites par fonctions fournies graine/cible | acceptée |
| [ADR-006](adr/0006-age-pour-le-backend-de-secrets-local.md) | age pour le backend de secrets local | acceptée |
| [ADR-007](adr/0007-cle-maitresse-en-fichier-local.md) | Clé maîtresse en fichier local | acceptée (dette) |
| [ADR-008](adr/0008-profil-connected-uniquement-en-iteration-1.md) | Profil `connected` uniquement en itération 1 | acceptée (dette) |
| [ADR-009](adr/0009-licence-apache-2-0.md) | Licence Apache 2.0 | acceptée |
| [ADR-010](adr/0010-nom-du-projet-genesis.md) | Nom du projet : Genesis | acceptée |
| [ADR-011](adr/0011-architecture-coeur-modules-pas-de-monolithe.md) | Architecture cœur + modules, pas de monolithe | acceptée |
| [ADR-012](adr/0012-modules-en-processus-separes-via-hashicorp-go-plugin-grpc.md) | Modules en processus séparés via HashiCorp go-plugin (gRPC) | acceptée |
| [ADR-013](adr/0013-monorepo-artefacts-separes.md) | Monorepo, artefacts séparés | acceptée |
| [ADR-014](adr/0014-ports-declares-dans-les-manifests.md) | Ports déclarés dans les manifests | acceptée |
| [ADR-015](adr/0015-santhosh-tekuri-jsonschema-v5-pour-la-validation-json-schema.md) | `santhosh-tekuri/jsonschema/v5` pour la validation JSON Schema | acceptée |
| [ADR-016](adr/0016-core-ansible-v1-ouvert-a-tout-module-cible-pas-reserve-a-bas.md) | `core.ansible/v1` ouvert à tout module cible, pas réservé à `base-os` | acceptée |
| [ADR-017](adr/0017-modules-de-parc-une-fonction-peut-avoir-plusieurs-fournisseu.md) | Modules « de parc » : une fonction peut avoir plusieurs fournisseurs actifs simultanés | acceptée |
| [ADR-018](adr/0018-core-ansible-v1-par-certificat-openssh-pas-par-tsh.md) | `core.ansible/v1` par certificat OpenSSH, pas par `tsh` | acceptée |
| [ADR-019](adr/0019-repoint-ssh-vers-lagent-teleport-differe-hors-j8.md) | Repoint SSH vers l'agent Teleport différé hors J8 | acceptée |
| [ADR-020](adr/0020-la-graine-reste-active-jusqua-la-fin-retrait-automatique-si.md) | La graine reste active jusqu'à la fin, retrait automatique si tout est vert | acceptée |
| [ADR-021](adr/0021-processus-de-developpement-pr-atomiques-fusionnees-en-squash.md) | Processus de développement : PR atomiques fusionnées en squash, Release Please | acceptée ; suivi des jalons et de la dette complété par ADR-049 |
| [ADR-022](adr/0022-chemins-de-module-github-com-whiteroselk-genesis-go-mod-auto.md) | Chemins de module `github.com/WhiteRoseLK/genesis`, go.mod autonomes | acceptée |
| [ADR-049](adr/0049-jalons-sur-github-adr-numerotees-par-issue.md) | Jalons suivis sur GitHub, ADR en fichiers numérotés par leur issue | acceptée |
