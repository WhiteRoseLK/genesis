# ADR-018 — `core.ansible/v1` par certificat OpenSSH, pas par `tsh`

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Contexte : une fois l'agent Teleport (`ssh_service`) installé sur une VM (J8), sshd natif doit être coupé pour que « SSH direct refusé » (doc08) soit vrai — ce qui casserait les futurs appels `core.ansible/v1` (connexion SSH directe avec la clé privée de service) vers cette VM si le cœur ne bascule pas lui-même vers un chemin passant par Teleport (doc07 : « le cœur bascule ses propres runners SSH en ProxyJump par le bastion »). Décision initiale envisagée : `internal/runner` apprend un `ProxyCommand tsh proxy ssh`, ce qui suppose le binaire `tsh` disponible dans le conteneur qui exécute Ansible (`willhallonline/ansible`, qui ne l'a pas) — implique soit une image Ansible personnalisée, soit un montage du binaire `tsh` depuis une image Teleport à chaque appel.

Vérifié manuellement dans Docker : le service `ssh_service` de Teleport (le node/agent) parle **SSH standard** sur son propre port (3022) — pas un protocole propriétaire. Un client OpenSSH classique (pas `tsh`) s'y connecte directement avec un certificat utilisateur signé par la CA Teleport (`tctl auth sign --format=openssh`), au format `<clé>-cert.pub` qu'OpenSSH reconnaît nativement à côté de la clé privée. `tsh`/le proxy multiplexé (3080) ne sont nécessaires que pour joindre un node non accessible directement au réseau (accès depuis l'extérieur via tunnel inversé) — non pertinent ici, toutes les VM du parc sont sur le même réseau privé.

Décision : `ansiblev1.Target` (`core.ansible/v1`) gagne un champ optionnel `ssh_certificate_pem` (ajout additif) ; quand renseigné, le certificat est écrit à côté de la clé privée au format `<keyfile>-cert.pub` avant de lancer le conteneur Ansible existant, sans aucune modification d'image. Conséquence : pas de dépendance à `tsh` dans le pipeline Ansible, changement minimal du cœur pour porter le « Repoint SSH » du doc07.
