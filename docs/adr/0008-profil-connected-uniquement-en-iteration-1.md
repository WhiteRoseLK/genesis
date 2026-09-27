# ADR-008 — Profil `connected` uniquement en itération 1

- **Statut** : acceptée (dette)
- **Débat** : antérieur au processus par issue (ADR-049)

Les images, paquets et conteneurs sont téléchargés depuis Internet. Toute URL de téléchargement doit néanmoins passer par un composant `artifacts` unique pour permettre le mode air-gap sans refonte.
