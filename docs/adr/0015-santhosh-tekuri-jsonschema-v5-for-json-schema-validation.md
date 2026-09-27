# ADR-015 — `santhosh-tekuri/jsonschema/v5` for JSON Schema validation

- **Status**: accepted
- **Discussion**: predates the issue-based process (ADR-049)

Context: doc 04 requires structural validation of the spec with JSON Schema (M1), and doc 03 has each module publish its `config_schema` as JSON Schema (M3) — both need the same validation mechanism. Alternatives: hand-written Go validation (no dependency, but logic duplicated between the overall envelope and each module's `config_schema`, and error messages would likely diverge); `xeipuuv/gojsonschema` (less maintained, draft-4 only). Decision: `santhosh-tekuri/jsonschema/v5`, pure Go, draft 2020-12 support, no cgo. Consequence: a dependency added to the root module (`internal/spec`) from M1, reused as is by the core in M3 to validate the modules' `config_schema`.
