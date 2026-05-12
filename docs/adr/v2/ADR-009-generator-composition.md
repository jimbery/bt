# ADR-009: Generator composition — trace-derived distributions and schema-derived generators

**Status:** Proposed  
**Date:** 2026-05-12

---

## Context

The property strategy (M4) generates test inputs by deriving Rapid generators from OpenAPI schema types. This produces uniformly distributed, schema-valid inputs — which is correct but naive. A `currency` field typed as `string` will generate arbitrary strings, not the three values that actually appear in production traffic.

The trace adapter (M12) produces a `TraceProfile` containing, for each operation argument: observed value distributions (e.g. `GBP: 70%, USD: 25%, EUR: 5%`), numeric ranges (e.g. `amount` observed between 10–500), null rates, and always-present flags.

A decision is needed on how these two sources — trace-derived distributions and schema-derived generators — compose when both are available. Several failure modes must be handled: the profile covers some operations but not all; the profile covers an argument but with too few samples to be statistically meaningful; the observed distribution contradicts the schema (e.g. observed values that are schema-invalid); or the profile is absent entirely.

---

## Decision

Use a **layered generator model**. The schema-derived generator is the base layer; the trace-derived distribution is an optional refinement layer that wraps it. Composition is per-argument, not per-operation.

The composition rules, in priority order:

1. **Trace distribution present and sufficient (≥ 20 samples):** generate from the observed distribution weighted by frequency. Values that appear in the distribution but fail schema validation are silently dropped from the generator; if all observed values are invalid, fall back to rule 3 with a warning.
2. **Trace range present (numeric arguments), distribution absent or insufficient:** use the observed `[min, max]` range as bounds on the schema-derived numeric generator instead of the schema's `minimum`/`maximum` (or unbounded if the schema has none). The schema type still determines whether the output is `integer` or `number`.
3. **No trace data for this argument, or profile absent:** use the schema-derived generator unchanged — identical behaviour to M4 with no trace profile.

The null rate from the trace profile overrides the schema's `nullable` flag only when the schema permits null. If the schema marks a field as required/non-null, null is never generated regardless of what the trace profile observed.

The `always_present` flag in the trace profile promotes optional schema fields to always-generated when set to `true`. This captures the pattern where a field is technically optional per the schema but always sent in practice.

The composition is implemented in a `ComposedGenerator` type:

```go
// internal/strategy/property/gen/composed.go

type ComposedGenerator struct {
    schema    *openapi3.Schema
    traceArg  *model.ArgumentProfile  // nil if no trace data
    rapid     *rapid.Generator[any]
}

func NewComposedGenerator(schema *openapi3.Schema, traceArg *model.ArgumentProfile) *ComposedGenerator

func (g *ComposedGenerator) Draw(t *rapid.T, label string) any
```

`Draw` implements the composition rules above. The schema generator is always constructed — it acts as the fallback and as the validator for trace-observed values.

---

## Rationale

**Per-argument composition is the right granularity.** Operations often have a mix of constrained arguments (e.g. `currency` has a small enum-like distribution in practice) and free-form arguments (e.g. `description` is arbitrary text). Applying trace data only where it is meaningful and falling back to schema generation elsewhere produces better coverage than either pure approach.

**The schema generator must remain the fallback.** The trace profile reflects past behaviour — it cannot anticipate new schema fields, API version changes, or argument combinations that never appeared in the captured traffic window. A tool that only generates what it has seen before is a regression test, not a property test. Schema-derived generation ensures the test surface extends beyond observed usage.

**Minimum sample threshold (20) prevents overfit.** A distribution derived from 3 observed values is not statistically meaningful and would produce tests that are barely different from replay. Twenty samples is a conservative threshold — low enough not to exclude meaningful small APIs, high enough to produce a usable distribution shape.

**Invalid observed values must be silently dropped, not hard-errored.** Production traffic sometimes contains values that the schema has since tightened (e.g. a currency code that is no longer accepted). The trace profile should not block test runs because the API evolved after the HAR was captured. Dropping invalid values with a warning log is the least-surprise behaviour.

**Null rate from trace can only narrow, not widen, nullability.** If the schema says a field is non-nullable, the API contract guarantees it. Overriding that from observed traffic would generate inputs the server is contractually permitted to reject, producing false failures.

---

## Consequences

- `ComposedGenerator` is the only generator type the property strategy instantiates. When no trace profile is present, `traceArg` is nil and `Draw` behaves identically to the M4 pure-schema generator.
- The property strategy logs at `DEBUG` level for each argument: which composition rule was applied and why. This is the primary diagnostic tool when trace-informed generation produces unexpected results.
- `ComposedGenerator.Draw` must validate each trace-observed value against the schema before including it in the distribution. This validation must not be skipped for performance — distributions are built at strategy startup, not per draw.
- The `always_present` promotion applies to `rapid.Optional`-wrapped generators. When `always_present` is true, the optional wrapper is removed and the argument is always generated.
- A new `bt analyze --validate` flag runs the trace profile through schema validation and reports any observed values that would be dropped, without running a test. This gives teams visibility before committing a profile.

---

## Test contract (written before implementation)

The following test cases define the acceptance boundary for this ADR. They are written in M12.5 and must pass before M12 is considered complete.

```go
// internal/strategy/property/gen/composed_test.go

func TestComposedGenerator(t *testing.T) {
    schema := stringSchema() // type: string, no enum

    t.Run("no trace data: generates arbitrary strings", func(t *testing.T) {
        gen := NewComposedGenerator(schema, nil)
        values := drawN(t, gen, 100)
        // Should produce varied strings, not a fixed set
        assert.Greater(t, uniqueCount(values), 5)
    })

    t.Run("trace distribution with sufficient samples: draws from distribution", func(t *testing.T) {
        traceArg := &model.ArgumentProfile{
            Type:     "string",
            Samples:  repeat([]string{"GBP", "USD", "EUR"}, 30), // 90 samples
            Distribution: map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05},
        }
        gen := NewComposedGenerator(schema, traceArg)
        values := drawN(t, gen, 1000)
        // All values must be from the observed distribution
        for _, v := range values {
            assert.Contains(t, []string{"GBP", "USD", "EUR"}, v)
        }
        // GBP should be the most frequent, within statistical tolerance
        assert.Greater(t, frequencyOf("GBP", values), 0.55)
    })

    t.Run("trace distribution with insufficient samples (<20): falls back to schema generator", func(t *testing.T) {
        traceArg := &model.ArgumentProfile{
            Samples:      []string{"GBP", "USD"},
            Distribution: map[string]float64{"GBP": 1.0},
        }
        gen := NewComposedGenerator(schema, traceArg)
        values := drawN(t, gen, 100)
        assert.Greater(t, uniqueCount(values), 2, "should not be locked to observed values")
    })

    t.Run("all trace-observed values are schema-invalid: falls back to schema generator with warning", func(t *testing.T) {
        enumSchema := stringEnumSchema([]string{"GBP", "USD"}) // schema only allows GBP, USD
        traceArg := &model.ArgumentProfile{
            Samples:      repeat([]string{"INVALID"}, 30),
            Distribution: map[string]float64{"INVALID": 1.0},
        }
        gen := NewComposedGenerator(enumSchema, traceArg)
        values := drawN(t, gen, 100)
        for _, v := range values {
            assert.Contains(t, []string{"GBP", "USD"}, v)
        }
    })

    t.Run("numeric range from trace applied to integer schema", func(t *testing.T) {
        intSchema := integerSchema() // no min/max in schema
        traceArg := &model.ArgumentProfile{
            Type:  "integer",
            Range: &model.Range{Min: 10, Max: 500},
        }
        gen := NewComposedGenerator(intSchema, traceArg)
        values := drawN(t, gen, 200)
        for _, v := range values {
            assert.GreaterOrEqual(t, v.(int64), int64(10))
            assert.LessOrEqual(t, v.(int64), int64(500))
        }
    })

    t.Run("always_present promotes optional field to always generated", func(t *testing.T) {
        optSchema := optionalStringSchema()
        traceArg := &model.ArgumentProfile{AlwaysPresent: true}
        gen := NewComposedGenerator(optSchema, traceArg)
        values := drawN(t, gen, 100)
        for _, v := range values {
            assert.NotNil(t, v)
        }
    })

    t.Run("null_rate from trace: schema non-nullable field is never nil", func(t *testing.T) {
        nonNullSchema := requiredStringSchema()
        traceArg := &model.ArgumentProfile{NullRate: 0.5} // trace observed 50% nulls
        gen := NewComposedGenerator(nonNullSchema, traceArg)
        values := drawN(t, gen, 100)
        for _, v := range values {
            assert.NotNil(t, v, "schema non-nullable must not be overridden by trace null rate")
        }
    })
}
```