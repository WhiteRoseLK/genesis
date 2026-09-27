# ADR-008 — `connected` profile only in iteration 1

- **Status**: accepted (debt)
- **Discussion**: predates the issue-based process (ADR-049)

Images, packages and containers are downloaded from the Internet. Every download URL must nevertheless go through a single `artifacts` component, so that air-gapped mode can be added without a redesign.
