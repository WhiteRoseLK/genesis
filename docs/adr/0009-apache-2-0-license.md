# ADR-009 — Apache 2.0 license

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

A permissive license: use, modification and commercial integration are allowed, including by integrators, with an obligation to keep the notice and a patent clause protecting contributors and users. Rejected alternative: AGPL-3.0, which protects against a closed SaaS takeover but slows enterprise adoption. Consequences: an Apache 2.0 `LICENSE` file and a `NOTICE` file at the root, an SPDX header `// SPDX-License-Identifier: Apache-2.0` in every source file, dependencies with compatible licenses only (no GPL/AGPL in the binary).
