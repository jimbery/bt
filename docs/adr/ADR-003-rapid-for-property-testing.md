# ADR-003: Rapid for property-based testing

**Status:** Proposed  
**Date:** 2026-05-09

---

## Context

Property-based testing requires a generator framework that can produce structured inputs, shrink failures to minimal reproducible cases, and integrate naturally with Go's testing model. Several options exist in the Go ecosystem.

Options considered:

- **Rapid** (`pgregory.net/rapid`) — modern Go library, structured generators, automatic shrinking, state-machine support, active maintenance
- **gopter** — older, less ergonomic API, shrinking support but more verbose generator definitions, less active
- **go-quickcheck** — minimal, closest to Haskell QuickCheck origins, limited shrinking, no state-machine support
- **Manual implementation** — full control but significant upfront cost with no meaningful advantage

## Decision

Use Rapid as the property-based testing engine for the property strategy.

## Rationale

- **Shrinking** is non-negotiable for a tool aimed at backend testing. A minimal reproducible failure is significantly more useful than the raw generated input that first triggered it. Rapid's shrinking is automatic and requires no extra author effort
- **Structured generators** allow schema-derived input generation. Rapid's generator composition model maps well to JSON Schema and OpenAPI type hierarchies
- **State-machine support** means the graph/state strategy (deferred) can share the same generator infrastructure rather than requiring a separate framework
- **Active maintenance** and clean API surface reduce the risk of being blocked by library issues on a solo project
- Rapid integrates with `go test` naturally, which keeps the property strategy consistent with Go's standard testing model

## Consequences

- Schema-derived generators must be built on top of Rapid's primitives. The OpenAPI → generator mapping is non-trivial, particularly for `oneOf`, `anyOf`, nullable fields, and circular references. A spike on this mapping should be completed before M4 begins in earnest
- Rapid runs within `go test` — the property strategy will invoke it through the standard testing interface, which has implications for how the engine captures and formats results
- Seeds produced by Rapid are deterministic and portable, satisfying the replay requirement from M3