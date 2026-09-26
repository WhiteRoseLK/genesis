# Politique de sécurité

Genesis manipule des secrets, une PKI et des accès SSH à toute une infrastructure : les vulnérabilités sont prises au sérieux.

## Signaler une vulnérabilité

**N'ouvrez pas d'issue publique.** Utilisez le signalement privé de GitHub : onglet **Security** du dépôt → **Report a vulnerability** ([lien direct](https://github.com/WhiteRoseLK/genesis/security/advisories/new)).

Indiquez si possible :

- la version ou le commit concerné ;
- le composant (couche du cœur ou module) ;
- un scénario de reproduction minimal, **sans secret réel** ;
- l'impact estimé.

Un accusé de réception est donné sous 7 jours. Le correctif est préparé dans un avis de sécurité privé, puis publié avec une version corrigée et un crédit au découvreur s'il le souhaite.

## Versions prises en charge

Tant que le projet est en `0.x`, seule la dernière version publiée (et `main`) reçoit des correctifs de sécurité.

## Périmètre

Sont notamment concernés : fuite de secret (journaux, état, erreurs, fichiers temporaires), contournement du broker (appel à une fonction non déclarée dans `requires`), élévation de privilèges sur la graine, falsification de module installé (`genesis.lock`), chaîne d'approvisionnement (images, dépendances).
