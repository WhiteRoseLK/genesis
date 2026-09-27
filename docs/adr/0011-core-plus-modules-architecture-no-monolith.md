# ADR-011 — Core + modules architecture, no monolith

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Context: add products and, later, the physical layer without a redesign. Decision: a generic core; one module per product; interactions only through versioned functions routed by a broker; build order derived from dependencies, no hard-coded layer. Consequences: an SDK to maintain, discipline in versioning function APIs, a mandatory conformance suite.
