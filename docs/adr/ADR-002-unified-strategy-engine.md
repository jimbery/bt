# ADR-002: Unified strategy engine

**Status:** Proposed  
**Date:** 2026-05-09

---

## Context

`bt` supports multiple testing strategies: table-driven, property-based, fuzz, contract, and graph/state. Without a shared execution model these would become five separate tools that happen to share a binary — each with its own artifact format, report shape, CLI flags, and replay mechanism. This fragments the user experience and makes the codebase expensive to maintain as new strategies are added.

## Decision

All strategy modes compile down to a shared execution lifecycle. Every strategy produces the same `Case`, `Result`, and `Artifact` types. The engine, reporter, and replay system are strategy-agnostic.

The shared lifecycle is:

1. Resolve target and operations
2. Build cases or sequences
3. Execute requests and hooks
4. Evaluate invariants
5. Minimise or shrink on failure where supported
6. Emit artifacts, summaries, and CI outputs

Each strategy implements a common interface:

```go
type Strategy interface {
    Name() StrategyKind
    Plan(ctx context.Context, spec StrategySpec, ops []Operation) ([]Case, error)
    Execute(ctx context.Context, cases []Case, exec Executor) ([]Result, error)
}
```

## Rationale

- A single lifecycle means replay, reporting, and artifact storage are written once and inherited by every strategy automatically
- Users learn one mental model regardless of which strategy they are running
- New strategies are additive — they implement the interface and slot into the existing engine without touching the reporter or replay system
- The exit criterion for M3 (replay bundles) is that all later strategies inherit reproducibility for free; this is only possible with a unified model

## Consequences

- Some strategies (graph/state, fuzz) have execution semantics that do not map cleanly to a flat list of cases. The `Case` type must be expressive enough to represent sequences and stateful transitions without becoming a lowest-common-denominator type
- Strategy-specific config lives in `StrategySpec.Config` — this should use typed structs per strategy internally, not raw `map[string]any`, to preserve type safety at the engine boundary
- The interface must be stable before the plugin SDK (M8 deferred) is designed, since plugins will implement this same contract