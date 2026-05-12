# M10 implementation notes

Milestone: [m10-table-assertion-schema.md](./m10-table-assertion-schema.md).

Implemented in-tree (not kin-openapi): schemas use `pkg/model.SchemaRef`; response schema evaluation lives in `internal/strategy/table/schema_assertion.go` and reuses `internal/strategy/property/validate` plus additional-property warnings aligned with contract behaviour.

- **JSONPath** violations use `$` / `$.field` / `$.items[0].sku` style.
- **`$ref` resolution** at table plan time uses `target_schema_path` on the strategy spec (set by `internal/runplan` from `cfg.Target.SchemaPath`, resolved with `filepath.Abs` when relative so it matches the working-directory semantics used elsewhere).
- **Public loaders** for tests and tooling: `table.LoadCasesFromReader`, `table.LoadCasesWithSpec`, `table.IsConfigError`.
- **Results**: `model.Result.SchemaViolations` (always encoded as a JSON array in `model.Result.MarshalJSON`); reporters surface `schema_violations` in JSON and human-readable console/JUnit output.

When extending behaviour, keep table status/header checks independent of schema checks (both run whenever expectations exist).
