# 06 — Secrets et état

## Principe
L'utilisateur ne fournit que les identifiants de l'hyperviseur (par référence). **Tout autre secret est généré, stocké et distribué par l'outil.**

## Interface

```go
type Ref string // ex. "pki/root-ca-key" — seul type circulant hors du store

type Store interface {
    Ensure(ctx context.Context, ref Ref, gen Generator, meta Meta) error // crée si absent
    Get(ctx context.Context, ref Ref) (Secret, error)                    // Secret redacte String()/MarshalJSON
    Put(ctx context.Context, ref Ref, s Secret, meta Meta) error
    List(ctx context.Context, prefix string) ([]Entry, error)           // métadonnées seulement
    Backend() string
}

type Meta struct {
    Owner      string    // capacité propriétaire
    Consumers  []string  // capacités / VM consommatrices
    Kind       string    // password | token | private-key | certificate | ssh-key
    CreatedAt  time.Time
    Rotation   string    // never | on-handover | 90d …
    Recovery   bool      // doit rester en copie locale après passation
}
```

Générateurs fournis : mot de passe (32 caractères, alphabet sûr), token aléatoire 256 bits, clé ECDSA P-384 / Ed25519, paire SSH Ed25519, certificat (via fonction `pki.issuer`).

## Backends
### `file` (graine, itération 1)
- Répertoire `state_dir/secrets/`, un fichier par secret, chiffré **age** avec la clé maîtresse ; métadonnées dans un fichier séparé non chiffré mais sans valeur secrète.
- Permissions `0700` répertoire, `0600` fichiers, propriétaire l'utilisateur de l'outil.
- Clé maîtresse : `state_dir/master.key` générée par `init`, **affichée une fois** avec un avertissement de sauvegarde.
- Écritures atomiques (fichier temporaire + rename).

### `vault` (cible)
- Moteur KV v2, chemin `genesis/<env>/<ref>`, métadonnées en custom metadata.
- Authentification de l'outil : AppRole dédié `genesis`, dont les identifiants sont eux-mêmes dans le backend `file`.

### Migration `file` → `vault`
Déclenchée par la passation de la capacité `secrets` : copie de chaque entrée, vérification par relecture, bascule du backend actif dans l'état. Les entrées `Recovery: true` restent dans `file`.

## Dette assumée (itération 1)
Clé maîtresse en clair sur le disque de la graine, token root et clés de descellement Vault conservés dans le backend `file`. L'interface `MasterKeyProvider` doit exister dès maintenant (implémentation `file` seule) pour brancher TPM / Shamir / HSM plus tard.

## Redaction
- Type `Secret` : `String()` et `MarshalJSON()` renvoient `***`.
- Handler `slog` filtrant les attributs de type `Secret` et les motifs connus (tokens Vault, clés PEM).
- Test automatisé : exécution e2e complète avec capture des logs et de l'état, recherche de chaque valeur secrète générée → doit être absente.

## État (`internal/state`)
- Fichier `state_dir/state.json`, versionné (`schemaVersion`), écriture atomique, verrou fichier pour interdire deux `apply` concurrents.
- Contenu : empreinte de la spec appliquée, VM (nom, id attribué par le module compute, IP), statut de chaque module et étape, fournisseur actif de chaque fonction, versions des modules, endpoints, backend secret actif, historique des passations.
- **Ne contient aucune valeur secrète**, uniquement des `Ref`.
- `genesis state export` produit une archive (état + secrets chiffrés) pour sauvegarde ou reprise depuis une autre graine.
- Plus tard : backend d'état dans Vault ou GitLab après passation.
