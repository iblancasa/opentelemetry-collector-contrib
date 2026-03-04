# Multi-PR Plan (Schemagen + Bundles)

Use this document as the reference checklist. When you’re ready for a given PR, point me to the branch and say “PR <n>”, and I’ll implement only that PR’s scope.

## PR 1 — Schema Generation Core Fixes
Goal: Improve schemagen fidelity for non-Go-native types and prevent “object” fallbacks where type info exists.

Changes:
- Add `cmd/schemagen/internal/external_types.go` to resolve external alias types using `go/types`.
- Extend AST helpers to parse raw tags and generate text for embedded type handling.
- Update `parser.go` to:
  - resolve external aliases before emitting `$ref`
  - treat interfaces/funcs as `any` (not nil)
  - return nil safely when parseExpr returns nil
  - collect structured issues (vs. print)
- Add `internal/report.go` and issue tracking in parser.
- Add/adjust tests: new testdata `test12` + parser tests updates.

Notes:
- This PR improves schema quality (e.g., more precise fields vs `object`).
- No bundle/validator changes in this PR.

## PR 2 — Validator CLI
Goal: Provide CLI validator for OTel configs and resolve `$ref` from local and module roots.

Changes:
- Add `cmd/schemagen/validator` CLI:
  - `./validator --config=<yaml> --schema=<bundle> --map <module>=<path>`
- Loader resolves:
  - module refs
  - relative refs
  - local `$defs` refs
- Register `format: duration` validator (Go `time.ParseDuration`)
- Unit tests for validator behavior and format handling.

## PR 3 — Bundlegen (Single Bundle Tool)
Goal: Build a single, self-contained schema bundle with inline `$defs` support.

Changes:
- Add `cmd/schemagen/bundlegen`:
  - `-all`, `-inline`, `-allow-missing`, `-map`, `-out`, `-t`
- Bundle includes schema refs for all components or only those in a config.
- Inlining resolves refs into `$defs`.
- Missing refs handled via `x-missingRef` when `-allow-missing`.

Notes:
- This is the “single schema” tool (one bundle file for validation).

## PR 4 — Sweep (Generate Missing Schemas)
Goal: Automated schema generation across core & contrib repos.

Changes:
- Add `cmd/schemagen/sweep`
  - scans for `metadata.yaml`
  - generates `config.schema.yaml`
  - outputs report file (issues + missing)
- Add `configDir` support in `.schemagen.yaml` overrides.
- Generate minimal empty schema for packages with no exported types.
- Skip known internal non-component dirs (e.g., `mdatagen`).

## PR 5 — Schema Strictness and Composition
Goal: Strict schema while still allowing composed configs (via `allOf`).

Changes:
- Default `additionalProperties: false` for object schemas.
- When embedded via `allOf`, remove `additionalProperties` on the embedded schema to avoid blocking sibling properties.
- This ensures composed configs (e.g., resourcedetection + confighttp) validate correctly.

## PR 6 — Full Bundle Generator Script
Goal: One command that regenerates everything and builds full bundle.

Changes:
- Add `cmd/schemagen/generate-full-bundle.sh`
  - runs `sweep` on core + contrib
  - iteratively re-runs bundlegen and auto-fixes missing refs via schemagen
  - supports `FORCE_REGEN=1` to regenerate all schemas
- Document it in `cmd/schemagen/GENERATE_BUNDLE.md`

## PR 7 — Web-Validator Compatibility
Goal: Make schema compatible with validators like jsonschemavalidator.net.

Changes:
- In bundlegen, replace `format: duration` with regex `pattern` and remove `format`.
- Allow null for component config objects (so `batch: null` is valid).
- Ensure `allOf` embedded schemas don’t block extra fields by removing `additionalProperties`.
- Add OTLP deprecated aliases in pipeline patterns: `otlp`, `otlpgrpc`, `otlphttp`.

## Artifacts
- Full bundle output: `/tmp/otel-collector-full-bundle.json`
- Validation CLI usage: `./validator --config=... --schema=... --map ...`

