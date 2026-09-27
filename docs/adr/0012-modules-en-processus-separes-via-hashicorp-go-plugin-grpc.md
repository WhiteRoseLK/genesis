# ADR-012 — Modules en processus séparés via HashiCorp go-plugin (gRPC)

- **Statut** : acceptée
- **Débat** : antérieur au processus par issue (ADR-049)

Alternatives : packages Go compilés dans le binaire (plus simple mais dérive rapide vers le monolithe, recompilation pour tout ajout) ; plugins Go natifs `plugin` (fragiles, versions de toolchain identiques exigées). Choix : go-plugin, éprouvé par Terraform, Packer et Vault. Avantages : isolation des pannes, versionnement indépendant, modules écrivables dans d'autres langages via le protobuf. Coût : latence négligeable ici, outillage protobuf.
