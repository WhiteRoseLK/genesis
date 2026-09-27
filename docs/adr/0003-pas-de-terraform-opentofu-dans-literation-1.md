# ADR-003 — Pas de Terraform/OpenTofu dans l'itération 1

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

La fonction `compute.vm/v1` est minimale (quelques opérations VM) ; une dépendance à Terraform ajouterait un second état à réconcilier. À réévaluer pour les providers riches (vSphere, cloud).
