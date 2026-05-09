# engine

Shared execution lifecycle for strategies will live here (plan → execute → report hooks), aligned with ADR-002.

Today, **`bt run` orchestrates** discovery, planning, execution, and reporting in `internal/cli/run.go`. This package holds placeholders until that logic moves out of the CLI surface.
