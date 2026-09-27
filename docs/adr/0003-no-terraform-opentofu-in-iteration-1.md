# ADR-003 — No Terraform/OpenTofu in iteration 1

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

The `compute.vm/v1` function is minimal (a few VM operations); a dependency on Terraform would add a second state to reconcile. To be reassessed for rich providers (vSphere, cloud).
