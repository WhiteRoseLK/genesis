# ADR-019 — Repoint SSH vers l'agent Teleport différé hors J8

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Contexte : doc08 exigeait pour J8 « SSH direct refusé ». Couper le sshd natif dans `fleet.agent/v1.Install` casserait tout appel `core.ansible/v1` ultérieur vers la VM (ex. `Handover` de `vault`) tant que les appelants n'utilisent pas le certificat Teleport sur le port 3022. Le mécanisme côté cœur existe (ADR-018 : `ansiblev1.Target.ssh_certificate_pem`, géré par `internal/broker`), mais personne ne l'alimente : il faudrait qu'un module obtienne un certificat utilisateur (`access.ssh/v1.SignUserKey`, aujourd'hui `Unimplemented`) et bascule sa cible (port, certificat) après l'enrôlement de l'agent — chantier transverse à tous les modules cible. Alternatives : (a) tout faire dans J8 — rejetée par l'utilisateur, périmètre trop large pour le jalon ; (b) couper sshd sans repoint — rejetée, casse les appels ultérieurs.

Décision (utilisateur) : `fleet.agent/v1.Install` installe et enrôle l'agent Teleport mais **laisse le sshd natif actif** ; `core.ansible/v1` continue à se connecter par clé sur le port 22. `Verify(teleport)` prouve une connexion SSH réelle via l'agent (certificat `tctl auth sign`), pas le refus de l'accès direct. Critère J8 ajusté (doc08), dette tracée par une issue dédiée.

Conséquence : J8 livre un Teleport fonctionnel sur tout le parc sans durcissement d'accès ; le repoint (alimentation de `ssh_certificate_pem`, `SignUserKey`, coupure sshd) reste à faire dans un jalon ultérieur.
