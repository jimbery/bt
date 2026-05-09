# ADR-006: OpenAPI-first adapter strategy

**Status:** Proposed  
**Date:** 2026-05-09

---

## Context

`bt` needs to discover operations, normalise schemas, and derive test inputs from API specifications. Multiple protocol formats are in scope: OpenAPI (REST), GraphQL, and gRPC (protobuf). A decision is needed on which adapter to build first and how the adapter boundary should be designed to support additional protocols without forcing all protocols into a single shape.

## Decision

Build the OpenAPI adapter first. Design the adapter interface to be protocol-native internally, normalising to the shared `Operation` model only at the boundary the engine requires. Additional adapters (GraphQL, gRPC) follow after the interface is proven against OpenAPI.

The adapter interface:

```go
type Adapter interface {
    Name() string
    Discover(ctx context.Context, target Target) ([]Operation, error)
    Validate(ctx context.Context, target Target) error
}
```

Adapters own discovery and normalisation only. Execution policy and test logic live in the engine and strategies.

## Rationale

- **OpenAPI provides the fastest MVP path** — it is the most widely adopted API specification format, has the strongest schema definition support, and maps most directly to the `Operation` model (method, path, parameters, request body, response schemas)
- **Schema-aware generation** — OpenAPI's type system is rich enough to drive property-based generators (ADR-003) from the schema directly, which is the core value proposition of M4
- **Protocol-native internal representation** — GraphQL queries and mutations are not operations in the REST sense. Forcing them into `Operation` too early produces a lossy, lowest-common-denominator model. Keeping protocol-native representations internally and normalising only at the engine boundary means each adapter can be as expressive as its protocol allows
- **Proven before extended** — the adapter interface should be validated against a real, complex OpenAPI spec (with `oneOf`, `anyOf`, nullable fields, circular refs) before GraphQL or gRPC adapters are designed. Discovering the hard edges with OpenAPI informs how the interface should flex

## Consequences

- The OpenAPI → `Operation` normalisation step is non-trivial. Real-world specs use `oneOf`, `anyOf`, nullable fields, discriminators, and circular references. A spike on normalisation should be completed early in M2 before the full adapter is built
- GraphQL's operation model (queries, mutations, fragments, variables, nested selections) is fundamentally different from REST. When the GraphQL adapter is built, the `Operation` type or the adapter interface may need to evolve. This is acceptable — the interface is internal until the plugin SDK is exposed
- gRPC (protobuf) is closer to OpenAPI structurally but streaming RPCs introduce an execution model that the current `Case` and `Result` types do not cover. Streaming support should be a conscious design decision when the gRPC adapter is scoped
- The trace adapter (production-traffic-derived seeds) is deferred and sits outside the schema-driven adapter model entirely; it should be treated as a separate input source rather than forced into the `Adapter` interface