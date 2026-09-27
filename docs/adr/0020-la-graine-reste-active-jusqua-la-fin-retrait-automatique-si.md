# ADR-020 — La graine reste active jusqu'à la fin, retrait automatique si tout est vert

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Contexte : jusqu'à J7, le moteur arrêtait un service graine (`SeedDown`) juste après la passation de la fonction correspondante, au milieu d'`apply` ; doc02/doc05 prévoyaient en plus une commande manuelle `genesis seed retire`, jamais implémentée. Logique voulue par l'utilisateur : tous les services graine démarrent ; dès qu'un service réel est prêt et vérifié (ex. DNS), il sert à tous les services construits ensuite ; la graine reste active pour ses propres besoins ; elle est décommissionnée à la fin, sans geste manuel, si tous les tests sont verts.

Alternatives : (a) garder `SeedDown` pendant `apply`, `seed retire` ne faisant que finaliser — rejetée : pas de filet de sécurité, la graine disparaît dès la passation ; (b) `SeedDown` hors d'`apply`, déclenché par `genesis seed retire` manuel — rejetée par l'utilisateur, le geste manuel n'apporte rien si les vérifications décident.

Décision : la passation (`Handover` + repoint) ne touche plus à la graine. En fin d'`apply`, `internal/engine.retireSeed` arrête toute la graine (ordre inverse du plan) si : chaque fonction graine a un fournisseur cible, les secrets ont migré vers Vault quand une capacité `secrets` existe, et un `Verify` final (`verify.final`) de chaque module cible est vert. L'état note `seed_retired` : un `apply` ultérieur n'interroge ni ne relance plus les modules graine, ne rejoue plus `Handover`, et la clé active de chaque fonction pointe directement la cible. La commande `genesis seed retire` est supprimée.

Conséquence : l'environnement se construit et s'autonomise en un seul `apply`. Une fonction graine sans relève cible conserve la graine (l'`apply` réussit, le journal le dit) ; un `Verify` final rouge fait échouer l'`apply` avec la graine intacte. Le passage du store fichier en lecture seule (doc05) n'est pas appliqué par le code à ce jalon (dette).
