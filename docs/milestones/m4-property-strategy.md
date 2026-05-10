# M4 — Property Strategy

This document follows the same structure as M1, M2, and M3: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist. Tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

---

## Overview

M4 delivers the core generative testing capability: property-based testing driven by Rapid, with schema-derived input generators, automatic shrinking, and seed-based deterministic replay. This is the milestone that turns `bt` from a deterministic test runner into a tool that can find bugs you didn't know to look for.

The four pieces built here are:

1. **Schema-derived generators** — maps OpenAPI type definitions to Rapid generators that produce valid and boundary-pushing inputs
2. **Invariant evaluators** — `no_5xx`, `response_matches_schema`, `idempotency_key_prevents_duplicates` — each checks the response object in full, not just the status code
3. **Property runner** — drives Rapid within the engine, captures shrunk failures, and writes artifacts
4. **`--seed` flag** — makes any run deterministic and replayable from a single integer

Each piece has its own spec, tests, and implementation section. Build and verify each step before moving to the next.

**Exit criterion:** `bt run --strategy property` finds real bugs in a target API and reproduces them deterministically from a seed. Every failure includes a shrunk minimal input, the full request/response pair, schema validation errors, and an artifact bundle.

---

## Step 1 — Schema-derived generators

### Spec

- The generator package lives at `internal/strategy/property/gen`
- Generators are produced from `model.Schema` — the normalised schema type already in the domain model (populated by the OpenAPI adapter in M2)
- `GenForSchema(s model.Schema) *rapid.Generator[any]` is the primary entry point; it returns a Rapid generator that produces values matching the schema
- Each JSON Schema primitive maps to a corresponding Rapid generator:
  - `string` → `rapid.StringOf(rapid.RuneFrom(validStringRunes))` with length bounds from `minLength`/`maxLength`
  - `integer` → `rapid.Int64Range(min, max)` bounded by `minimum`/`maximum` where present, otherwise `rapid.Int64()`
  - `number` → `rapid.Float64Range(min, max)` bounded similarly
  - `boolean` → `rapid.Bool()`
  - `array` → `rapid.SliceOfN(itemGen, minItems, maxItems)` bounded by `minItems`/`maxItems`
  - `object` → produces `map[string]any` by calling each property's generator; required properties are always included; optional properties are included randomly
  - `null` → a generator that always returns nil
  - `enum` → `rapid.SampledFrom(values)` — one of the enumerated values, never anything outside the set
- When `nullable: true` is set, the generator wraps the base generator with a 10% chance of returning nil
- Unknown or unsupported schema types return a generator that always produces nil and records a warning — they do not panic
- `oneOf` and `anyOf` are supported: `rapid.OneOf(gen1, gen2, ...)` across the sub-schema generators
- Circular references are detected and resolved with a depth limit of 5; beyond that the generator produces nil
- The generator package must not import anything from `internal/cli` or `internal/mcp`

### Tests

`internal/strategy/property/gen/gen_test.go`:

```go
package gen_test

import (
	"testing"

	pgrapid "pgregory.net/rapid"

	"github.com/yourorg/bt/internal/strategy/property/gen"
	"github.com/yourorg/bt/pkg/model"
)

// --- Helpers ---

// mustGenerate runs the generator once using rapid.MakeCustom and returns the
// produced value. It fails the test if the generator panics.
func mustGenerate(t *testing.T, g *pgrapid.Generator[any]) any {
	t.Helper()
	var result any
	pgrapid.MakeCustom(t, func(t *pgrapid.T) {
		result = g.Draw(t, "value")
	})
	return result
}

// mustGenerateN runs the generator n times and returns all results.
func mustGenerateN(t *testing.T, g *pgrapid.Generator[any], n int) []any {
	t.Helper()
	results := make([]any, n)
	pgrapid.MakeCustom(t, func(t *pgrapid.T) {
		for i := range results {
			results[i] = g.Draw(t, "value")
		}
	})
	return results
}

// --- String generator ---

func TestGenForSchema_String_ProducesString(t *testing.T) {
	s := model.Schema{Type: "string"}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		if _, ok := val.(string); !ok {
			t.Fatalf("expected string, got %T: %v", val, val)
		}
	})
}

func TestGenForSchema_String_RespectsMinLength(t *testing.T) {
	min := 5
	s := model.Schema{Type: "string", MinLength: &min}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v").(string)
		if len(val) < min {
			t.Fatalf("string length %d is below minLength %d: %q", len(val), min, val)
		}
	})
}

func TestGenForSchema_String_RespectsMaxLength(t *testing.T) {
	max := 10
	s := model.Schema{Type: "string", MaxLength: &max}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v").(string)
		if len(val) > max {
			t.Fatalf("string length %d exceeds maxLength %d: %q", len(val), max, val)
		}
	})
}

func TestGenForSchema_String_RespectsMinAndMaxLength(t *testing.T) {
	min, max := 3, 8
	s := model.Schema{Type: "string", MinLength: &min, MaxLength: &max}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v").(string)
		if len(val) < min || len(val) > max {
			t.Fatalf("string length %d not in [%d, %d]: %q", len(val), min, max, val)
		}
	})
}

// --- Integer generator ---

func TestGenForSchema_Integer_ProducesInt64(t *testing.T) {
	s := model.Schema{Type: "integer"}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		if _, ok := val.(int64); !ok {
			t.Fatalf("expected int64, got %T: %v", val, val)
		}
	})
}

func TestGenForSchema_Integer_RespectsMinimum(t *testing.T) {
	min := float64(0)
	s := model.Schema{Type: "integer", Minimum: &min}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v").(int64)
		if val < 0 {
			t.Fatalf("integer %d is below minimum 0", val)
		}
	})
}

func TestGenForSchema_Integer_RespectsMaximum(t *testing.T) {
	max := float64(100)
	s := model.Schema{Type: "integer", Maximum: &max}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v").(int64)
		if val > 100 {
			t.Fatalf("integer %d exceeds maximum 100", val)
		}
	})
}

func TestGenForSchema_Integer_RespectsMinimumAndMaximum(t *testing.T) {
	min, max := float64(1), float64(50)
	s := model.Schema{Type: "integer", Minimum: &min, Maximum: &max}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v").(int64)
		if val < 1 || val > 50 {
			t.Fatalf("integer %d not in [1, 50]", val)
		}
	})
}

// --- Number (float) generator ---

func TestGenForSchema_Number_ProducesFloat64(t *testing.T) {
	s := model.Schema{Type: "number"}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		if _, ok := val.(float64); !ok {
			t.Fatalf("expected float64, got %T: %v", val, val)
		}
	})
}

func TestGenForSchema_Number_RespectsMinimumAndMaximum(t *testing.T) {
	min, max := float64(-1.0), float64(1.0)
	s := model.Schema{Type: "number", Minimum: &min, Maximum: &max}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v").(float64)
		if val < -1.0 || val > 1.0 {
			t.Fatalf("float %f not in [-1.0, 1.0]", val)
		}
	})
}

// --- Boolean generator ---

func TestGenForSchema_Boolean_ProducesBool(t *testing.T) {
	s := model.Schema{Type: "boolean"}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		if _, ok := val.(bool); !ok {
			t.Fatalf("expected bool, got %T: %v", val, val)
		}
	})
}

func TestGenForSchema_Boolean_ProducesBothTrueAndFalse(t *testing.T) {
	// Over many draws the generator should produce both true and false.
	// This is a coverage check, not a distribution check.
	s := model.Schema{Type: "boolean"}
	g := gen.GenForSchema(s)
	sawTrue, sawFalse := false, false

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v").(bool)
		if val {
			sawTrue = true
		} else {
			sawFalse = true
		}
	})

	if !sawTrue || !sawFalse {
		t.Errorf("boolean generator did not produce both true and false over many draws")
	}
}

// --- Array generator ---

func TestGenForSchema_Array_ProducesSlice(t *testing.T) {
	s := model.Schema{
		Type:  "array",
		Items: &model.Schema{Type: "string"},
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		if _, ok := val.([]any); !ok {
			t.Fatalf("expected []any, got %T: %v", val, val)
		}
	})
}

func TestGenForSchema_Array_ItemsMatchSubSchema(t *testing.T) {
	s := model.Schema{
		Type:  "array",
		Items: &model.Schema{Type: "integer"},
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		items := g.Draw(t, "v").([]any)
		for i, item := range items {
			if _, ok := item.(int64); !ok {
				t.Fatalf("array item[%d]: expected int64, got %T: %v", i, item, item)
			}
		}
	})
}

func TestGenForSchema_Array_RespectsMinItems(t *testing.T) {
	min := 3
	s := model.Schema{
		Type:     "array",
		Items:    &model.Schema{Type: "boolean"},
		MinItems: &min,
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		items := g.Draw(t, "v").([]any)
		if len(items) < 3 {
			t.Fatalf("array length %d is below minItems 3", len(items))
		}
	})
}

func TestGenForSchema_Array_RespectsMaxItems(t *testing.T) {
	max := 5
	s := model.Schema{
		Type:     "array",
		Items:    &model.Schema{Type: "string"},
		MaxItems: &max,
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		items := g.Draw(t, "v").([]any)
		if len(items) > 5 {
			t.Fatalf("array length %d exceeds maxItems 5", len(items))
		}
	})
}

// --- Object generator ---

func TestGenForSchema_Object_ProducesMap(t *testing.T) {
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"name":   {Type: "string"},
			"amount": {Type: "integer"},
		},
		Required: []string{"name", "amount"},
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		if _, ok := val.(map[string]any); !ok {
			t.Fatalf("expected map[string]any, got %T: %v", val, val)
		}
	})
}

func TestGenForSchema_Object_AlwaysIncludesRequiredProperties(t *testing.T) {
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"id":          {Type: "string"},
			"amount":      {Type: "integer"},
			"description": {Type: "string"}, // optional
		},
		Required: []string{"id", "amount"},
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		m := g.Draw(t, "v").(map[string]any)
		for _, req := range []string{"id", "amount"} {
			if _, ok := m[req]; !ok {
				t.Fatalf("required property %q missing from generated object: %v", req, m)
			}
		}
	})
}

func TestGenForSchema_Object_RequiredPropertyTypesMatchSubSchema(t *testing.T) {
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"count":   {Type: "integer"},
			"enabled": {Type: "boolean"},
			"label":   {Type: "string"},
		},
		Required: []string{"count", "enabled", "label"},
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		m := g.Draw(t, "v").(map[string]any)

		if _, ok := m["count"].(int64); !ok {
			t.Fatalf("property 'count': expected int64, got %T", m["count"])
		}
		if _, ok := m["enabled"].(bool); !ok {
			t.Fatalf("property 'enabled': expected bool, got %T", m["enabled"])
		}
		if _, ok := m["label"].(string); !ok {
			t.Fatalf("property 'label': expected string, got %T", m["label"])
		}
	})
}

func TestGenForSchema_Object_OptionalPropertiesHaveCorrectTypeWhenPresent(t *testing.T) {
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"required_field": {Type: "string"},
			"optional_int":   {Type: "integer"},
		},
		Required: []string{"required_field"},
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		m := g.Draw(t, "v").(map[string]any)
		if val, ok := m["optional_int"]; ok {
			if _, ok := val.(int64); !ok {
				t.Fatalf("optional property 'optional_int': expected int64 when present, got %T", val)
			}
		}
	})
}

// --- Enum generator ---

func TestGenForSchema_Enum_ProducesOnlyEnumeratedValues(t *testing.T) {
	allowed := []any{"pending", "complete", "cancelled"}
	s := model.Schema{
		Type: "string",
		Enum: allowed,
	}
	g := gen.GenForSchema(s)

	allowedSet := map[any]bool{"pending": true, "complete": true, "cancelled": true}

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		if !allowedSet[val] {
			t.Fatalf("enum generator produced value %q not in allowed set", val)
		}
	})
}

func TestGenForSchema_Enum_NeverProducesValueOutsideSet(t *testing.T) {
	// Belt-and-suspenders: run many draws and confirm the set is never violated.
	allowed := []any{1, 2, 3}
	s := model.Schema{Type: "integer", Enum: allowed}
	g := gen.GenForSchema(s)
	allowedSet := map[any]bool{int64(1): true, int64(2): true, int64(3): true}

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		if !allowedSet[val] {
			t.Fatalf("enum generator produced %v which is not in %v", val, allowed)
		}
	})
}

// --- Nullable ---

func TestGenForSchema_Nullable_CanProduceNil(t *testing.T) {
	s := model.Schema{Type: "string", Nullable: true}
	g := gen.GenForSchema(s)

	sawNil := false
	for i := 0; i < 200; i++ {
		val := mustGenerate(t, g)
		if val == nil {
			sawNil = true
			break
		}
	}

	if !sawNil {
		t.Error("nullable string generator never produced nil over 200 draws")
	}
}

func TestGenForSchema_Nullable_CanProduceNonNil(t *testing.T) {
	s := model.Schema{Type: "string", Nullable: true}
	g := gen.GenForSchema(s)

	sawNonNil := false
	for i := 0; i < 200; i++ {
		val := mustGenerate(t, g)
		if val != nil {
			sawNonNil = true
			break
		}
	}

	if !sawNonNil {
		t.Error("nullable string generator never produced a non-nil value over 200 draws")
	}
}

// --- OneOf / AnyOf ---

func TestGenForSchema_OneOf_ProducesValueMatchingOneSubSchema(t *testing.T) {
	s := model.Schema{
		OneOf: []model.Schema{
			{Type: "string"},
			{Type: "integer"},
		},
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		switch val.(type) {
		case string, int64:
			// OK
		default:
			t.Fatalf("oneOf generator produced unexpected type %T: %v", val, val)
		}
	})
}

func TestGenForSchema_AnyOf_ProducesValueMatchingOneSubSchema(t *testing.T) {
	s := model.Schema{
		AnyOf: []model.Schema{
			{Type: "boolean"},
			{Type: "number"},
		},
	}
	g := gen.GenForSchema(s)

	pgrapid.Check(t, func(t *pgrapid.T) {
		val := g.Draw(t, "v")
		switch val.(type) {
		case bool, float64:
			// OK
		default:
			t.Fatalf("anyOf generator produced unexpected type %T: %v", val, val)
		}
	})
}

// --- Unknown types ---

func TestGenForSchema_UnknownType_DoesNotPanic(t *testing.T) {
	// A schema with an unrecognised type must not panic.
	s := model.Schema{Type: "bytes"}
	g := gen.GenForSchema(s)
	// Simply calling Draw should not panic.
	mustGenerate(t, g)
}

func TestGenForSchema_UnknownType_ProducesNil(t *testing.T) {
	s := model.Schema{Type: "bytes"}
	g := gen.GenForSchema(s)
	val := mustGenerate(t, g)
	if val != nil {
		t.Errorf("unknown type should produce nil, got %T: %v", val, val)
	}
}

// --- Circular reference guard ---

func TestGenForSchema_CircularReference_DoesNotPanic(t *testing.T) {
	// Construct a schema that self-references to depth 10 (beyond the limit of 5).
	// The generator should resolve this gracefully.
	// We simulate this by building a deeply nested object manually.
	innermost := model.Schema{Type: "string"}
	wrapped := innermost
	for i := 0; i < 10; i++ {
		wrapped = model.Schema{
			Type:       "object",
			Properties: map[string]model.Schema{"child": wrapped},
			Required:   []string{"child"},
		}
	}
	g := gen.GenForSchema(wrapped)
	// Must not panic.
	mustGenerate(t, g)
}
```

### Implementation

`internal/strategy/property/gen/gen.go`:

```go
package gen

import (
	"pgregory.net/rapid"

	"github.com/yourorg/bt/pkg/model"
)

const maxDepth = 5

// GenForSchema returns a Rapid generator that produces values conforming to s.
// Unknown types produce nil. Circular references are cut off at maxDepth.
func GenForSchema(s model.Schema) *rapid.Generator[any] {
	return genWithDepth(s, 0)
}

func genWithDepth(s model.Schema, depth int) *rapid.Generator[any] {
	if depth > maxDepth {
		return rapid.Just[any](nil)
	}

	// Enum takes precedence over type.
	if len(s.Enum) > 0 {
		return rapid.SampledFrom(s.Enum)
	}

	// oneOf / anyOf.
	if len(s.OneOf) > 0 {
		return oneOfGen(s.OneOf, depth)
	}
	if len(s.AnyOf) > 0 {
		return oneOfGen(s.AnyOf, depth)
	}

	var base *rapid.Generator[any]
	switch s.Type {
	case "string":
		base = stringGen(s)
	case "integer":
		base = intGen(s)
	case "number":
		base = numberGen(s)
	case "boolean":
		base = rapid.Map(rapid.Bool(), func(b bool) any { return b })
	case "array":
		base = arrayGen(s, depth)
	case "object":
		base = objectGen(s, depth)
	case "null":
		base = rapid.Just[any](nil)
	default:
		// Unrecognised type: produce nil, do not panic.
		return rapid.Just[any](nil)
	}

	if s.Nullable {
		return rapid.OneOf(rapid.Just[any](nil), base)
	}
	return base
}
```

---

## Step 2 — Response schema validator

### Spec

The schema validator is separate from the generator — it checks that a real API response body conforms to a `model.Schema`. This is what makes assertions meaningful beyond status codes.

- Package: `internal/strategy/property/validate`
- `ValidateResponse(body []byte, schema model.Schema) []SchemaViolation` — unmarshals the body and checks every field
- `SchemaViolation` carries: `Path string`, `Message string`, `Expected string`, `Got string`
- Validation rules:
  - **Type checking**: checks that each field in the response matches the declared type in the schema
  - **Required fields**: all fields listed in `required` must be present in the response body; absence is a violation
  - **Extra fields**: fields present in the response but absent from `properties` are reported as warnings (not violations) unless `additionalProperties: false`
  - **Enum values**: if a field declares `enum`, the response value must be one of them
  - **Minimum / maximum**: numeric fields are checked against declared bounds
  - **Nested objects**: validation recurses into nested objects and arrays
  - **Null safety**: a non-nullable field with a null value in the response is a violation
- `ValidateResponse` never panics, even on malformed JSON; it returns a `SchemaViolation` with `Path: "$"` describing the parse error
- An empty slice means the response is schema-valid

### Tests

`internal/strategy/property/validate/validate_test.go`:

```go
package validate_test

import (
	"encoding/json"
	"testing"

	"github.com/yourorg/bt/internal/strategy/property/validate"
	"github.com/yourorg/bt/pkg/model"
)

// orderSchema is a realistic schema representing a successful order response.
// Tests use this as a baseline and modify it to create specific cases.
func orderSchema() model.Schema {
	return model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"id":          {Type: "string"},
			"amount":      {Type: "number"},
			"currency":    {Type: "string", Enum: []any{"GBP", "USD", "EUR"}},
			"status":      {Type: "string", Enum: []any{"pending", "complete", "cancelled"}},
			"description": {Type: "string"},
		},
		Required: []string{"id", "amount", "currency", "status"},
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("cannot marshal test value: %v", err)
	}
	return b
}

// --- Valid responses ---

func TestValidateResponse_ValidOrder_NoViolations(t *testing.T) {
	body := mustMarshal(t, map[string]any{
		"id":          "ord-001",
		"amount":      99.99,
		"currency":    "GBP",
		"status":      "pending",
		"description": "Test order",
	})

	violations := validate.ValidateResponse(body, orderSchema())
	if len(violations) != 0 {
		t.Errorf("expected no violations for a valid order, got %d: %v", len(violations), violations)
	}
}

func TestValidateResponse_ValidOrder_OptionalFieldAbsent_NoViolations(t *testing.T) {
	// 'description' is optional — its absence must not be a violation.
	body := mustMarshal(t, map[string]any{
		"id":       "ord-002",
		"amount":   10.0,
		"currency": "USD",
		"status":   "complete",
	})

	violations := validate.ValidateResponse(body, orderSchema())
	if len(violations) != 0 {
		t.Errorf("expected no violations when optional field is absent, got %d: %v", len(violations), violations)
	}
}

// --- Required field violations ---

func TestValidateResponse_MissingRequiredField_ReportsViolation(t *testing.T) {
	// 'status' is required but omitted.
	body := mustMarshal(t, map[string]any{
		"id":       "ord-003",
		"amount":   50.0,
		"currency": "EUR",
		// status intentionally omitted
	})

	violations := validate.ValidateResponse(body, orderSchema())
	if len(violations) == 0 {
		t.Fatal("expected a violation for missing required field 'status', got none")
	}

	found := false
	for _, v := range violations {
		if v.Path == "$.status" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected violation at path '$.status', violations were: %v", violations)
	}
}

func TestValidateResponse_MultipleRequiredFieldsMissing_ReportsAllViolations(t *testing.T) {
	// 'amount', 'currency', and 'status' are all required and all absent.
	body := mustMarshal(t, map[string]any{
		"id": "ord-004",
	})

	violations := validate.ValidateResponse(body, orderSchema())

	missingPaths := map[string]bool{
		"$.amount":   false,
		"$.currency": false,
		"$.status":   false,
	}
	for _, v := range violations {
		if _, tracked := missingPaths[v.Path]; tracked {
			missingPaths[v.Path] = true
		}
	}
	for path, seen := range missingPaths {
		if !seen {
			t.Errorf("expected violation for missing required field at path %q", path)
		}
	}
}

// --- Type violations ---

func TestValidateResponse_WrongType_String_ReportsViolation(t *testing.T) {
	// 'amount' should be a number but we send a string.
	body := mustMarshal(t, map[string]any{
		"id":       "ord-005",
		"amount":   "not-a-number",
		"currency": "GBP",
		"status":   "pending",
	})

	violations := validate.ValidateResponse(body, orderSchema())
	if len(violations) == 0 {
		t.Fatal("expected a type violation for 'amount', got none")
	}

	found := false
	for _, v := range violations {
		if v.Path == "$.amount" {
			found = true
			if v.Expected != "number" {
				t.Errorf("violation at $.amount: expected Expected=%q, got %q", "number", v.Expected)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected violation at path '$.amount', violations were: %v", violations)
	}
}

func TestValidateResponse_WrongType_Object_Instead_Of_String_ReportsViolation(t *testing.T) {
	// 'id' should be a string but we send an object.
	body := mustMarshal(t, map[string]any{
		"id":       map[string]any{"nested": true},
		"amount":   10.0,
		"currency": "GBP",
		"status":   "pending",
	})

	violations := validate.ValidateResponse(body, orderSchema())

	found := false
	for _, v := range violations {
		if v.Path == "$.id" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected type violation at '$.id', violations: %v", violations)
	}
}

// --- Enum violations ---

func TestValidateResponse_InvalidEnumValue_ReportsViolation(t *testing.T) {
	// 'currency' must be GBP, USD, or EUR. "XYZ" is not allowed.
	body := mustMarshal(t, map[string]any{
		"id":       "ord-006",
		"amount":   20.0,
		"currency": "XYZ",
		"status":   "pending",
	})

	violations := validate.ValidateResponse(body, orderSchema())
	if len(violations) == 0 {
		t.Fatal("expected an enum violation for 'currency', got none")
	}

	found := false
	for _, v := range violations {
		if v.Path == "$.currency" {
			found = true
			if v.Got != "XYZ" {
				t.Errorf("violation at $.currency: expected Got=%q, got %q", "XYZ", v.Got)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected violation at '$.currency', violations: %v", violations)
	}
}

func TestValidateResponse_ValidEnumValue_NoViolation(t *testing.T) {
	for _, currency := range []string{"GBP", "USD", "EUR"} {
		body := mustMarshal(t, map[string]any{
			"id":       "ord-007",
			"amount":   5.0,
			"currency": currency,
			"status":   "pending",
		})

		violations := validate.ValidateResponse(body, orderSchema())
		for _, v := range violations {
			if v.Path == "$.currency" {
				t.Errorf("currency %q is valid but produced violation: %v", currency, v)
			}
		}
	}
}

// --- Numeric bounds ---

func TestValidateResponse_IntegerBelowMinimum_ReportsViolation(t *testing.T) {
	min := float64(0)
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"count": {Type: "integer", Minimum: &min},
		},
		Required: []string{"count"},
	}
	body := mustMarshal(t, map[string]any{"count": -1})

	violations := validate.ValidateResponse(body, s)
	if len(violations) == 0 {
		t.Fatal("expected violation for value below minimum, got none")
	}

	found := false
	for _, v := range violations {
		if v.Path == "$.count" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected violation at '$.count', violations: %v", violations)
	}
}

func TestValidateResponse_NumberAboveMaximum_ReportsViolation(t *testing.T) {
	max := float64(100)
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"score": {Type: "number", Maximum: &max},
		},
		Required: []string{"score"},
	}
	body := mustMarshal(t, map[string]any{"score": 101.5})

	violations := validate.ValidateResponse(body, s)
	if len(violations) == 0 {
		t.Fatal("expected violation for value above maximum, got none")
	}

	found := false
	for _, v := range violations {
		if v.Path == "$.score" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected violation at '$.score', violations: %v", violations)
	}
}

// --- Nested objects ---

func TestValidateResponse_NestedObject_RequiredFieldMissing_ReportsViolationWithFullPath(t *testing.T) {
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"order": {
				Type: "object",
				Properties: map[string]model.Schema{
					"id":     {Type: "string"},
					"amount": {Type: "number"},
				},
				Required: []string{"id", "amount"},
			},
		},
		Required: []string{"order"},
	}
	// 'amount' inside 'order' is missing.
	body := mustMarshal(t, map[string]any{
		"order": map[string]any{"id": "ord-001"},
	})

	violations := validate.ValidateResponse(body, s)

	found := false
	for _, v := range violations {
		if v.Path == "$.order.amount" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected violation at '$.order.amount', violations: %v", violations)
	}
}

func TestValidateResponse_NestedObject_WrongType_ReportsViolationWithFullPath(t *testing.T) {
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"meta": {
				Type: "object",
				Properties: map[string]model.Schema{
					"page": {Type: "integer"},
				},
				Required: []string{"page"},
			},
		},
		Required: []string{"meta"},
	}
	body := mustMarshal(t, map[string]any{
		"meta": map[string]any{"page": "first"}, // string instead of integer
	})

	violations := validate.ValidateResponse(body, s)

	found := false
	for _, v := range violations {
		if v.Path == "$.meta.page" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected violation at '$.meta.page', violations: %v", violations)
	}
}

// --- Null safety ---

func TestValidateResponse_NullValueForNonNullableField_ReportsViolation(t *testing.T) {
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"id": {Type: "string", Nullable: false},
		},
		Required: []string{"id"},
	}
	body := mustMarshal(t, map[string]any{"id": nil})

	violations := validate.ValidateResponse(body, s)
	if len(violations) == 0 {
		t.Fatal("expected violation for null value in non-nullable field, got none")
	}

	found := false
	for _, v := range violations {
		if v.Path == "$.id" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected violation at '$.id', violations: %v", violations)
	}
}

func TestValidateResponse_NullValueForNullableField_NoViolation(t *testing.T) {
	s := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"description": {Type: "string", Nullable: true},
		},
	}
	body := mustMarshal(t, map[string]any{"description": nil})

	violations := validate.ValidateResponse(body, s)
	for _, v := range violations {
		if v.Path == "$.description" {
			t.Errorf("nullable field should not produce a violation, got: %v", v)
		}
	}
}

// --- Malformed input ---

func TestValidateResponse_MalformedJSON_ReportsRootViolation(t *testing.T) {
	violations := validate.ValidateResponse([]byte("this is not json"), orderSchema())
	if len(violations) == 0 {
		t.Fatal("expected a violation for malformed JSON, got none")
	}
	if violations[0].Path != "$" {
		t.Errorf("expected root path '$' for parse error, got %q", violations[0].Path)
	}
}

func TestValidateResponse_EmptyBody_ReportsViolation(t *testing.T) {
	violations := validate.ValidateResponse([]byte{}, orderSchema())
	if len(violations) == 0 {
		t.Fatal("expected a violation for empty body, got none")
	}
}

func TestValidateResponse_NullBody_NeverPanics(t *testing.T) {
	// Should not panic on nil input.
	violations := validate.ValidateResponse(nil, orderSchema())
	// We don't assert a specific outcome beyond no panic — the body is invalid JSON.
	_ = violations
}

// --- Violation structure ---

func TestSchemaViolation_Fields_ArePopulated(t *testing.T) {
	body := mustMarshal(t, map[string]any{
		"id":       "ord-001",
		"amount":   "wrong-type",
		"currency": "GBP",
		"status":   "pending",
	})

	violations := validate.ValidateResponse(body, orderSchema())

	var amountViolation *validate.SchemaViolation
	for i := range violations {
		if violations[i].Path == "$.amount" {
			amountViolation = &violations[i]
			break
		}
	}

	if amountViolation == nil {
		t.Fatal("expected violation at '$.amount'")
	}
	if amountViolation.Message == "" {
		t.Error("violation Message must not be empty")
	}
	if amountViolation.Expected == "" {
		t.Error("violation Expected must not be empty")
	}
	if amountViolation.Got == "" {
		t.Error("violation Got must not be empty")
	}
}
```

---

## Step 3 — Built-in invariants

### Spec

Invariants are pure functions: they receive a `model.Result` (which includes the full request, response, and response schema) and return zero or more failures. They do not make network calls.

- Package: `internal/strategy/property/invariant`
- Three invariants are implemented in M4:

**`no_5xx`**
- Fails if `result.Response.StatusCode >= 500`
- `Failure.Invariant = "no_5xx"`
- `Failure.Message` includes the actual status code
- `Failure.Expected = "< 500"`, `Failure.Actual = "<status code>"`
- Passes for all 1xx, 2xx, 3xx, 4xx responses

**`response_matches_schema`**
- Fails if the response body does not conform to the operation's response schema for the returned status code
- Uses `validate.ValidateResponse` internally
- Each `SchemaViolation` produces a separate `model.Failure`
- `Failure.Invariant = "response_matches_schema"`
- `Failure.Path` is populated with the JSON path of the violation (e.g. `$.status`)
- `Failure.Expected` describes the expected type or value
- `Failure.Actual` contains the actual value found in the response
- Skips validation (returns no failures) when no schema is defined for the response status code
- Passes when the response body is a valid JSON object matching the schema

**`idempotency_key_prevents_duplicates`**
- Verifies that sending the same request twice with the same `Idempotency-Key` header produces identical responses (same status code, same response body)
- This invariant is only evaluated when `result.Input.Headers["Idempotency-Key"]` is non-empty
- On the second call, if the status code differs from the first: failure with `Failure.Invariant = "idempotency_key_prevents_duplicates"`
- On the second call, if the response body differs: failure with same invariant name
- This invariant requires the runner to supply both the first and second response; it takes a `model.IdempotencyResult` rather than a plain `model.Result`
- Skipped (no failures) when the input has no `Idempotency-Key` header

- Each invariant is a function matching the signature defined by `Invariant` in the invariant package
- Invariants must not panic on any input, including empty bodies and nil fields
- A registry maps invariant names (strings from config) to invariant functions

### Tests

`internal/strategy/property/invariant/invariant_test.go`:

```go
package invariant_test

import (
	"testing"

	"github.com/yourorg/bt/internal/strategy/property/invariant"
	"github.com/yourorg/bt/pkg/model"
)

// --- Helpers ---

func resultWithStatus(code int) model.Result {
	return model.Result{
		Response: model.ResponseDetail{
			StatusCode: code,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       []byte(`{"id":"ord-001","amount":10.0,"currency":"GBP","status":"pending"}`),
		},
	}
}

func resultWithBody(code int, body []byte) model.Result {
	return model.Result{
		Response: model.ResponseDetail{
			StatusCode: code,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       body,
		},
	}
}

// --- no_5xx ---

func TestNo5xx_200_Passes(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(200))
	if len(failures) != 0 {
		t.Errorf("expected no failures for HTTP 200, got %d: %v", len(failures), failures)
	}
}

func TestNo5xx_201_Passes(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(201))
	if len(failures) != 0 {
		t.Errorf("expected no failures for HTTP 201, got %d", len(failures))
	}
}

func TestNo5xx_400_Passes(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(400))
	if len(failures) != 0 {
		t.Errorf("expected no failures for HTTP 400, got %d", len(failures))
	}
}

func TestNo5xx_404_Passes(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(404))
	if len(failures) != 0 {
		t.Errorf("expected no failures for HTTP 404, got %d", len(failures))
	}
}

func TestNo5xx_499_Passes(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(499))
	if len(failures) != 0 {
		t.Errorf("expected no failures for HTTP 499, got %d", len(failures))
	}
}

func TestNo5xx_500_Fails(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(500))
	if len(failures) == 0 {
		t.Fatal("expected a failure for HTTP 500, got none")
	}
}

func TestNo5xx_503_Fails(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(503))
	if len(failures) == 0 {
		t.Fatal("expected a failure for HTTP 503, got none")
	}
}

func TestNo5xx_599_Fails(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(599))
	if len(failures) == 0 {
		t.Fatal("expected a failure for HTTP 599, got none")
	}
}

func TestNo5xx_Failure_HasCorrectInvariantName(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(500))
	if failures[0].Invariant != "no_5xx" {
		t.Errorf("expected Invariant=%q, got %q", "no_5xx", failures[0].Invariant)
	}
}

func TestNo5xx_Failure_ExpectedIsLessThan500(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(502))
	if failures[0].Expected != "< 500" {
		t.Errorf("expected Expected=%q, got %q", "< 500", failures[0].Expected)
	}
}

func TestNo5xx_Failure_ActualContainsStatusCode(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(502))
	if failures[0].Actual != "502" {
		t.Errorf("expected Actual=%q, got %q", "502", failures[0].Actual)
	}
}

func TestNo5xx_Failure_MessageIsNotEmpty(t *testing.T) {
	failures := invariant.No5xx(resultWithStatus(500))
	if failures[0].Message == "" {
		t.Error("failure message must not be empty")
	}
}

// --- response_matches_schema ---

func schemaResult(body []byte, schema model.Schema) model.Result {
	return model.Result{
		Operation: model.Operation{
			ResponseSchemas: map[int]model.Schema{
				200: schema,
			},
		},
		Response: model.ResponseDetail{
			StatusCode: 200,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       body,
		},
	}
}

func TestResponseMatchesSchema_ValidBody_Passes(t *testing.T) {
	schema := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"id":     {Type: "string"},
			"amount": {Type: "number"},
		},
		Required: []string{"id", "amount"},
	}
	body := []byte(`{"id":"ord-001","amount":99.99}`)

	failures := invariant.ResponseMatchesSchema(schemaResult(body, schema))
	if len(failures) != 0 {
		t.Errorf("expected no failures for valid body, got %d: %v", len(failures), failures)
	}
}

func TestResponseMatchesSchema_MissingRequiredField_Fails(t *testing.T) {
	schema := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"id":     {Type: "string"},
			"amount": {Type: "number"},
		},
		Required: []string{"id", "amount"},
	}
	// 'amount' is missing.
	body := []byte(`{"id":"ord-001"}`)

	failures := invariant.ResponseMatchesSchema(schemaResult(body, schema))
	if len(failures) == 0 {
		t.Fatal("expected failure for missing required field, got none")
	}

	found := false
	for _, f := range failures {
		if f.Path == "$.amount" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected failure at path '$.amount', failures: %v", failures)
	}
}

func TestResponseMatchesSchema_WrongType_Fails(t *testing.T) {
	schema := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"count": {Type: "integer"},
		},
		Required: []string{"count"},
	}
	body := []byte(`{"count":"not-an-integer"}`)

	failures := invariant.ResponseMatchesSchema(schemaResult(body, schema))
	if len(failures) == 0 {
		t.Fatal("expected failure for wrong type, got none")
	}

	found := false
	for _, f := range failures {
		if f.Path == "$.count" && f.Invariant == "response_matches_schema" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected failure at '$.count' with invariant 'response_matches_schema', failures: %v", failures)
	}
}

func TestResponseMatchesSchema_EachViolationIsASeparateFailure(t *testing.T) {
	schema := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"id":     {Type: "string"},
			"amount": {Type: "number"},
			"status": {Type: "string", Enum: []any{"pending", "complete"}},
		},
		Required: []string{"id", "amount", "status"},
	}
	// All three fields have problems: amount is wrong type, status is invalid enum, id is missing.
	body := []byte(`{"amount":"wrong","status":"unknown"}`)

	failures := invariant.ResponseMatchesSchema(schemaResult(body, schema))
	if len(failures) < 3 {
		t.Errorf("expected at least 3 failures (one per violation), got %d: %v", len(failures), failures)
	}
}

func TestResponseMatchesSchema_NoSchemaForStatusCode_Skipped(t *testing.T) {
	// The operation has no schema for a 404 response.
	result := model.Result{
		Operation: model.Operation{
			ResponseSchemas: map[int]model.Schema{
				200: {Type: "object", Properties: map[string]model.Schema{"id": {Type: "string"}}},
			},
		},
		Response: model.ResponseDetail{
			StatusCode: 404,
			Body:       []byte(`{"error":"not found"}`),
		},
	}

	failures := invariant.ResponseMatchesSchema(result)
	if len(failures) != 0 {
		t.Errorf("expected no failures when no schema defined for status code, got %d: %v", len(failures), failures)
	}
}

func TestResponseMatchesSchema_MalformedBody_Fails(t *testing.T) {
	schema := model.Schema{Type: "object"}
	body := []byte(`not valid json`)

	failures := invariant.ResponseMatchesSchema(schemaResult(body, schema))
	if len(failures) == 0 {
		t.Fatal("expected failure for malformed JSON body, got none")
	}
}

func TestResponseMatchesSchema_Failure_InvariantNameIsCorrect(t *testing.T) {
	schema := model.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]model.Schema{
			"id": {Type: "string"},
		},
	}
	body := []byte(`{}`)

	failures := invariant.ResponseMatchesSchema(schemaResult(body, schema))
	for _, f := range failures {
		if f.Invariant != "response_matches_schema" {
			t.Errorf("expected Invariant=%q, got %q", "response_matches_schema", f.Invariant)
		}
	}
}

// --- idempotency_key_prevents_duplicates ---

func idempotencyResult(key string, first, second model.ResponseDetail) model.IdempotencyResult {
	return model.IdempotencyResult{
		IdempotencyKey: key,
		First:          first,
		Second:         second,
	}
}

func TestIdempotencyKeyPrevents_SameStatusAndBody_Passes(t *testing.T) {
	body := []byte(`{"id":"ord-001","amount":10.0}`)
	result := idempotencyResult(
		"idem-key-abc",
		model.ResponseDetail{StatusCode: 201, Body: body},
		model.ResponseDetail{StatusCode: 201, Body: body},
	)

	failures := invariant.IdempotencyKeyPrevents(result)
	if len(failures) != 0 {
		t.Errorf("expected no failures for identical responses, got %d: %v", len(failures), failures)
	}
}

func TestIdempotencyKeyPrevents_DifferentStatusCode_Fails(t *testing.T) {
	body := []byte(`{"id":"ord-001"}`)
	result := idempotencyResult(
		"idem-key-def",
		model.ResponseDetail{StatusCode: 201, Body: body},
		model.ResponseDetail{StatusCode: 500, Body: []byte(`{"error":"internal"}`)},
	)

	failures := invariant.IdempotencyKeyPrevents(result)
	if len(failures) == 0 {
		t.Fatal("expected failure when status codes differ, got none")
	}
	if failures[0].Invariant != "idempotency_key_prevents_duplicates" {
		t.Errorf("expected Invariant=%q, got %q", "idempotency_key_prevents_duplicates", failures[0].Invariant)
	}
}

func TestIdempotencyKeyPrevents_DifferentBody_Fails(t *testing.T) {
	result := idempotencyResult(
		"idem-key-ghi",
		model.ResponseDetail{StatusCode: 201, Body: []byte(`{"id":"ord-001"}`)},
		model.ResponseDetail{StatusCode: 201, Body: []byte(`{"id":"ord-002"}`)},
	)

	failures := invariant.IdempotencyKeyPrevents(result)
	if len(failures) == 0 {
		t.Fatal("expected failure when response bodies differ, got none")
	}
}

func TestIdempotencyKeyPrevents_StatusAndBodyBothDiffer_ReportsBothFailures(t *testing.T) {
	result := idempotencyResult(
		"idem-key-jkl",
		model.ResponseDetail{StatusCode: 201, Body: []byte(`{"id":"ord-001"}`)},
		model.ResponseDetail{StatusCode: 409, Body: []byte(`{"error":"conflict"}`)},
	)

	failures := invariant.IdempotencyKeyPrevents(result)
	if len(failures) < 2 {
		t.Errorf("expected at least 2 failures (status + body), got %d: %v", len(failures), failures)
	}
}

func TestIdempotencyKeyPrevents_EmptyKey_Skipped(t *testing.T) {
	// No Idempotency-Key means the invariant does not apply.
	result := idempotencyResult(
		"", // empty key
		model.ResponseDetail{StatusCode: 201, Body: []byte(`{"id":"ord-001"}`)},
		model.ResponseDetail{StatusCode: 500, Body: []byte(`{"error":"internal"}`)},
	)

	failures := invariant.IdempotencyKeyPrevents(result)
	if len(failures) != 0 {
		t.Errorf("expected no failures when Idempotency-Key is empty, got %d", len(failures))
	}
}

// --- Registry ---

func TestRegistry_KnownInvariant_ReturnsFunction(t *testing.T) {
	known := []string{"no_5xx", "response_matches_schema"}
	for _, name := range known {
		fn, ok := invariant.Lookup(name)
		if !ok {
			t.Errorf("expected invariant %q to be in registry", name)
		}
		if fn == nil {
			t.Errorf("expected non-nil function for invariant %q", name)
		}
	}
}

func TestRegistry_UnknownInvariant_ReturnsFalse(t *testing.T) {
	_, ok := invariant.Lookup("does_not_exist")
	if ok {
		t.Error("expected Lookup to return false for unknown invariant")
	}
}
```

---

## Step 4 — Property runner

### Spec

- Package: `internal/strategy/property`
- `Runner` implements `engine.Strategy`
- `Runner.Run(ctx context.Context, plan model.TestPlan) ([]model.Result, error)` drives Rapid for each operation in the plan
- For each operation:
  - Generates inputs using `gen.GenForSchema` against the operation's request body schema and path/query parameter schemas
  - Executes the HTTP request via the existing runner (`internal/runner`)
  - Evaluates all configured invariants against the result; evaluates `response_matches_schema` by default even when not explicitly configured
  - On invariant failure, Rapid shrinks the input to a minimal reproducing case before reporting
  - Writes a failure artifact via `replay.Writer` on any failure
  - On success, records the result but does not write an artifact
- The seed is configurable via `--seed` flag (passed through `model.RunConfig`); when zero, a random seed is used and logged so the run can be reproduced
- `Runner` honours `ctx` cancellation: if the context is cancelled mid-run, it stops cleanly and returns partial results
- The number of Rapid test cases per operation is configurable via `RunConfig.PropertyChecks` (default: 100)

### Tests

`internal/strategy/property/runner_test.go`:

```go
package property_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/yourorg/bt/internal/strategy/property"
	"github.com/yourorg/bt/pkg/model"
)

// serverAlways returns a handler that always responds with the given status and body.
func serverAlways(status int, body string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

// serverFlaky returns a handler that returns 200 on even calls and 500 on odd calls.
func serverFlaky() *httptest.Server {
	count := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.Header().Set("Content-Type", "application/json")
		if count%2 == 0 {
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"ord-001","status":"pending"}`))
		} else {
			w.WriteHeader(500)
			w.Write([]byte(`{"error":"transient failure"}`))
		}
	}))
}

func planWithOperation(baseURL string, op model.Operation) model.TestPlan {
	return model.TestPlan{
		Target: model.Target{BaseURL: baseURL},
		Operations: []model.Operation{op},
		RunConfig: model.RunConfig{
			PropertyChecks: 20, // keep tests fast
			Seed:           42,
		},
	}
}

func TestRunner_StableEndpoint_No5xx_Passes(t *testing.T) {
	srv := serverAlways(200, `{"id":"ord-001","status":"pending"}`)
	defer srv.Close()

	op := model.Operation{
		ID:     "GetOrder",
		Method: "GET",
		Path:   "/orders/ord-001",
		Invariants: []string{"no_5xx"},
	}
	plan := planWithOperation(srv.URL, op)

	runner := property.NewRunner(t.TempDir())
	results, err := runner.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	for _, r := range results {
		if len(r.Failures) != 0 {
			t.Errorf("expected no failures on stable endpoint, got: %v", r.Failures)
		}
	}
}

func TestRunner_5xxEndpoint_No5xx_Fails(t *testing.T) {
	srv := serverAlways(500, `{"error":"always fails"}`)
	defer srv.Close()

	op := model.Operation{
		ID:         "GetOrder",
		Method:     "GET",
		Path:       "/orders/ord-001",
		Invariants: []string{"no_5xx"},
	}
	plan := planWithOperation(srv.URL, op)

	runner := property.NewRunner(t.TempDir())
	results, err := runner.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Invariant == "no_5xx" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected no_5xx failure from a 500 endpoint, got none")
	}
}

func TestRunner_SchemaViolation_ResponseMatchesSchema_Fails(t *testing.T) {
	// The server returns a body missing required fields.
	srv := serverAlways(200, `{"id":"ord-001"}`) // 'amount' and 'status' missing
	defer srv.Close()

	schema := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"id":     {Type: "string"},
			"amount": {Type: "number"},
			"status": {Type: "string"},
		},
		Required: []string{"id", "amount", "status"},
	}

	op := model.Operation{
		ID:     "GetOrder",
		Method: "GET",
		Path:   "/orders/ord-001",
		ResponseSchemas: map[int]model.Schema{200: schema},
		// response_matches_schema is evaluated by default even without explicit config.
	}
	plan := planWithOperation(srv.URL, op)

	runner := property.NewRunner(t.TempDir())
	results, err := runner.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Invariant == "response_matches_schema" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected response_matches_schema failure for body missing required fields, got none")
	}
}

func TestRunner_SchemaViolationFailure_IncludesViolationPath(t *testing.T) {
	srv := serverAlways(200, `{"id":"ord-001"}`) // missing 'amount'
	defer srv.Close()

	schema := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"id":     {Type: "string"},
			"amount": {Type: "number"},
		},
		Required: []string{"id", "amount"},
	}

	op := model.Operation{
		ID:              "GetOrder",
		Method:          "GET",
		Path:            "/orders/ord-001",
		ResponseSchemas: map[int]model.Schema{200: schema},
	}
	plan := planWithOperation(srv.URL, op)

	runner := property.NewRunner(t.TempDir())
	results, err := runner.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Invariant == "response_matches_schema" && f.Path == "$.amount" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected failure at path '$.amount', results: %v", results)
	}
}

func TestRunner_SameSeed_ProducesSameResults(t *testing.T) {
	// Two runs with the same seed must produce the same sequence of inputs.
	srv := serverAlways(200, `{"id":"ord-001","amount":10.0,"currency":"GBP","status":"pending"}`)
	defer srv.Close()

	schema := model.Schema{
		Type: "object",
		Properties: map[string]model.Schema{
			"amount":   {Type: "number", Minimum: func() *float64 { v := float64(1); return &v }()},
			"currency": {Type: "string"},
		},
		Required: []string{"amount", "currency"},
	}

	op := model.Operation{
		ID:     "CreateOrder",
		Method: "POST",
		Path:   "/orders",
		RequestBodySchema: &schema,
		Invariants: []string{"no_5xx"},
	}

	plan1 := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{op},
		RunConfig:  model.RunConfig{PropertyChecks: 10, Seed: 1234},
	}
	plan2 := plan1 // same seed

	runner := property.NewRunner(t.TempDir())
	results1, _ := runner.Run(context.Background(), plan1)
	results2, _ := runner.Run(context.Background(), plan2)

	if len(results1) != len(results2) {
		t.Fatalf("expected same number of results: got %d and %d", len(results1), len(results2))
	}
	// Both runs should agree on pass/fail for each result.
	for i := range results1 {
		if len(results1[i].Failures) != len(results2[i].Failures) {
			t.Errorf("result[%d]: failure count differs between seeded runs", i)
		}
	}
}

func TestRunner_ContextCancellation_StopsCleanly(t *testing.T) {
	srv := serverAlways(200, `{"id":"ord-001"}`)
	defer srv.Close()

	op := model.Operation{
		ID:         "GetOrder",
		Method:     "GET",
		Path:       "/orders/ord-001",
		Invariants: []string{"no_5xx"},
	}
	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{op},
		RunConfig:  model.RunConfig{PropertyChecks: 1000, Seed: 1},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	runner := property.NewRunner(t.TempDir())
	_, err := runner.Run(ctx, plan)
	// Should return without hanging. Error may be context.Canceled or nil depending on timing.
	_ = err
}

func TestRunner_Failure_ArtifactIsWritten(t *testing.T) {
	artifactDir := t.TempDir()
	srv := serverAlways(500, `{"error":"always fails"}`)
	defer srv.Close()

	op := model.Operation{
		ID:         "GetOrder",
		Method:     "GET",
		Path:       "/orders/ord-001",
		Invariants: []string{"no_5xx"},
	}
	plan := planWithOperation(srv.URL, op)
	plan.RunConfig.ArtifactDir = artifactDir

	runner := property.NewRunner(artifactDir)
	runner.Run(context.Background(), plan)

	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		t.Fatalf("cannot read artifact dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one artifact to be written on failure, got none")
	}
}

func TestRunner_NoFailure_NoArtifactIsWritten(t *testing.T) {
	artifactDir := t.TempDir()
	srv := serverAlways(200, `{"id":"ord-001","status":"pending"}`)
	defer srv.Close()

	op := model.Operation{
		ID:         "GetOrder",
		Method:     "GET",
		Path:       "/orders/ord-001",
		Invariants: []string{"no_5xx"},
	}
	plan := planWithOperation(srv.URL, op)

	runner := property.NewRunner(artifactDir)
	runner.Run(context.Background(), plan)

	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		t.Fatalf("cannot read artifact dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no artifacts on a passing run, got %d", len(entries))
	}
}
```

---

## Step 5 — `--seed` flag and CLI integration

### Spec

- `bt run --strategy property --seed 1234` passes seed `1234` to `model.RunConfig`
- When `--seed` is not provided, the engine generates a random seed, logs it at `INFO` level, and uses it for the run — the logged seed can be passed to `--seed` to reproduce the run exactly
- The seed is included in every artifact bundle so a replay bundle is self-documenting
- `bt run --strategy property --checks 500` overrides the default number of Rapid test cases per operation (default: 100)
- Both flags are documented in `bt run --help`

### Tests

`internal/cli/run_property_flags_test.go`:

```go
package cli_test

import (
	"bytes"
	"testing"

	"github.com/yourorg/bt/internal/cli"
)

func TestRunCommand_PropertyFlags_SeedParsed(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	// Invoke with --seed — must not return an unknown flag error.
	cmd.SetArgs([]string{"run", "--strategy", "property", "--seed", "9999", "--help"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error parsing --seed flag: %v", err)
	}
}

func TestRunCommand_PropertyFlags_ChecksParsed(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"run", "--strategy", "property", "--checks", "500", "--help"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("unexpected error parsing --checks flag: %v", err)
	}
}

func TestRunCommand_PropertyFlags_SeedAppearsInHelp(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"run", "--help"})
	cmd.Execute()

	if !bytes.Contains(buf.Bytes(), []byte("--seed")) {
		t.Error("expected '--seed' to appear in 'bt run --help' output")
	}
}

func TestRunCommand_PropertyFlags_ChecksAppearsInHelp(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)

	cmd.SetArgs([]string{"run", "--help"})
	cmd.Execute()

	if !bytes.Contains(buf.Bytes(), []byte("--checks")) {
		t.Error("expected '--checks' to appear in 'bt run --help' output")
	}
}
```

---

## Step 6 — Failure reporting

### Spec

The console reporter is updated to render property failures with richer detail than table failures:

- The shrunk minimal input that triggered the failure is printed
- Each schema violation is listed as a separate line with its JSON path, expected type/value, and actual value
- The seed is printed on every property run (pass or fail) so it can be used to reproduce
- Format for a property failure:

```
  FAIL  CreateOrder [property]        seed: 9999  (83 cases, shrunk 4 times)
       no_5xx: HTTP 500 — expected < 500, got 500
       response_matches_schema:
         $.amount   expected: number   got: "not-a-number"
         $.status   missing required field
       shrunk input:
         body: {"amount": "not-a-number", "currency": "GBP"}
       artifact: .bt/artifacts/2026-05-09T143022Z-CreateOrder.json
       replay:   bt replay .bt/artifacts/2026-05-09T143022Z-CreateOrder.json
```

### Tests

`internal/report/property_reporter_test.go`:

```go
package report_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yourorg/bt/internal/report"
	"github.com/yourorg/bt/pkg/model"
)

func propertyFailureResult() model.Result {
	return model.Result{
		CaseID:       "CreateOrder",
		StrategyKind: "property",
		Seed:         9999,
		CasesRun:     83,
		ShrinkCount:  4,
		Failures: []model.Failure{
			{
				Invariant: "no_5xx",
				Message:   "HTTP 500 — expected < 500, got 500",
				Expected:  "< 500",
				Actual:    "500",
			},
			{
				Invariant: "response_matches_schema",
				Path:      "$.amount",
				Message:   "type mismatch",
				Expected:  "number",
				Got:       `"not-a-number"`,
			},
			{
				Invariant: "response_matches_schema",
				Path:      "$.status",
				Message:   "missing required field",
				Expected:  "present",
				Got:       "absent",
			},
		},
		ShrunkInput: model.ShrunkInput{
			Body: []byte(`{"amount":"not-a-number","currency":"GBP"}`),
		},
		ArtifactPath: ".bt/artifacts/2026-05-09T143022Z-CreateOrder.json",
	}
}

func TestPropertyReporter_Failure_PrintsFAIL(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{propertyFailureResult()})

	if !strings.Contains(buf.String(), "FAIL") {
		t.Error("expected 'FAIL' in reporter output")
	}
}

func TestPropertyReporter_Failure_PrintsOperationID(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{propertyFailureResult()})

	if !strings.Contains(buf.String(), "CreateOrder") {
		t.Error("expected operation ID 'CreateOrder' in reporter output")
	}
}

func TestPropertyReporter_Failure_PrintsSeed(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{propertyFailureResult()})

	if !strings.Contains(buf.String(), "9999") {
		t.Error("expected seed '9999' in reporter output")
	}
}

func TestPropertyReporter_Failure_PrintsSchemaViolationPath(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{propertyFailureResult()})

	if !strings.Contains(buf.String(), "$.amount") {
		t.Error("expected schema violation path '$.amount' in reporter output")
	}
}

func TestPropertyReporter_Failure_PrintsExpectedAndGot(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{propertyFailureResult()})

	output := buf.String()
	if !strings.Contains(output, "number") {
		t.Error("expected 'number' (expected type) in reporter output")
	}
}

func TestPropertyReporter_Failure_PrintsShrunkInput(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{propertyFailureResult()})

	if !strings.Contains(buf.String(), "shrunk input") {
		t.Error("expected 'shrunk input' section in reporter output")
	}
}

func TestPropertyReporter_Failure_PrintsArtifactPath(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{propertyFailureResult()})

	if !strings.Contains(buf.String(), ".bt/artifacts/") {
		t.Error("expected artifact path in reporter output")
	}
}

func TestPropertyReporter_Failure_PrintsReplayCommand(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{propertyFailureResult()})

	if !strings.Contains(buf.String(), "bt replay") {
		t.Error("expected 'bt replay' command hint in reporter output")
	}
}

func TestPropertyReporter_Pass_PrintsSeedOnPassingRun(t *testing.T) {
	// Even passing property runs should print the seed so the run is reproducible.
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{{
		CaseID:       "GetOrder",
		StrategyKind: "property",
		Seed:         5555,
		CasesRun:     100,
		Failures:     nil,
	}})

	if !strings.Contains(buf.String(), "5555") {
		t.Error("expected seed to appear in passing property run output")
	}
}
```

---

## Local verification

```bash
# Run all unit tests
go test ./internal/strategy/property/... -race -v
go test ./internal/strategy/property/gen/... -race -v
go test ./internal/strategy/property/validate/... -race -v
go test ./internal/strategy/property/invariant/... -race -v
go test ./internal/report/... -race -v
go test ./internal/cli/... -race -v

# Build and smoke test
go build -o bt ./cmd/bt
./bt run --strategy property --seed 42 --checks 50 --config examples/orders-api/bt/backendtest.yaml
```

---

## M4 exit criterion

`bt run --strategy property` finds real bugs in a target API and reproduces them deterministically from a seed. Every failure includes:

- The shrunk minimal input that triggered it
- The full request and response captured in the artifact
- Per-field schema validation failures with JSON path, expected type/value, and actual value
- The seed and shrink count so the failure can be reproduced exactly

All unit tests pass with `-race`. The orders API broken endpoint is found automatically without any hand-written assertions.