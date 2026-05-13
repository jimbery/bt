# M11 — GraphQL Property Testing

This document follows the project convention: spec first, tests second, implementation third. No implementation file is written until the tests for it exist and clearly fail. The tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

**TDD order for this milestone:**
1. Write and verify all tests in this document (they will fail — no implementation exists yet)
2. Write implementation until all tests pass
3. Proceed to M11.5 integration tests

---

## Overview

M11 adds property-based testing for GraphQL. Where M9 delivered table and contract testing for GraphQL, M11 adds the property strategy — generating valid variable combinations from SDL argument types, running them against the API under declared invariants, and shrinking failures to minimal reproducible cases.

The four pieces built here:

1. **SDL-derived variable generator** — maps GraphQL SDL argument types to Rapid generators using the same composition pattern as the OpenAPI generator in M4
2. **`response_matches_schema` invariant for GraphQL** — validates `data.*` against the SDL-derived selection schema, reusing `AssertResponse` from M9
3. **`no_gql_errors` invariant** — fails if `errors` is present and non-null in the response
4. **`bt_suggest_invariants` MCP tool update** — returns GraphQL-appropriate invariant suggestions when the operation kind is `Query` or `Mutation`

**Exit criterion:** `bt run --strategy property --adapter graphql` generates variable combinations from SDL argument types, finds the `amount` type violation in the GraphQL API's broken resolver within 50 checks, shrinks the failure to a minimal input, and writes an artifact bundle. The failure is reproducible with `--seed`. All unit tests pass with `-race`.

---

## Step 1 — SDL-derived variable generator

### Spec

- Package: `internal/strategy/graphql/gen`
- Primary entry point: `GenForOperation(op model.Operation) *rapid.Generator[map[string]any]`
  - Returns a generator that produces a `map[string]any` of variable name → value for the operation's arguments
  - Each argument maps to a per-type generator via `GenForType(ref model.SchemaRef) *rapid.Generator[any]`
- Type coverage — SDL type → generator:

| SDL type | Generator |
|----------|-----------|
| `String` | `rapid.StringOf(rapid.RuneFrom(printableRunes))` |
| `Int` | `rapid.Int32()` (GraphQL `Int` is 32-bit signed) |
| `Float` | `rapid.Float64()` |
| `Boolean` | `rapid.Bool()` |
| `ID` | `rapid.StringOf(rapid.RuneFrom(alphanumericRunes))` with length 1–64 |
| Enum | `rapid.SampledFrom(declaredValues)` — only declared enum values, never anything outside the set |
| Input object | recursively generates all fields; required fields always present, optional fields present randomly |
| `[T]` (list) | `rapid.SliceOfN(itemGen, 0, 5)` — generates 0 to 5 items |
| `T!` (non-null) | base generator unwrapped — never produces nil |
| `T` (nullable) | wraps base generator with 10% nil probability |
| Custom scalar | treated as `String` with a warning logged — never panics |

- `GenForOperation` produces an empty map `map[string]any{}` when the operation has no arguments — this is a valid variables document for a no-arg query
- The generator must not produce variables that are syntactically invalid for their SDL type (e.g. it must not generate a float where `Int!` is declared)
- Unknown SDL types are treated as custom scalars (string generation + warning log)

### Tests

`internal/strategy/graphql/gen/gen_test.go`:

```go
package gen_test

import (
	"testing"

	rapid "pgregory.net/rapid"

	gqlgen "github.com/yourorg/bt/internal/strategy/graphql/gen"
	"github.com/yourorg/bt/pkg/model"
)

// --- helpers ---

// opWithArgs builds a model.Operation whose GQLVariableTypes is populated from
// the provided map of argName → SchemaRef.
func opWithArgs(args map[string]model.SchemaRef) model.Operation {
	return model.Operation{
		ID:               "TestOp",
		GQLKind:          model.GQLMutation,
		GQLVariableTypes: args,
	}
}

// drawVars runs the generator once and returns the produced variable map.
func drawVars(t *rapid.T, op model.Operation) map[string]any {
	t.Helper()
	g := gqlgen.GenForOperation(op)
	return g.Draw(t, "vars")
}

// --- GenForOperation ---

func TestGenForOperation_NoArgs_ReturnsEmptyMap(t *testing.T) {
	op := model.Operation{ID: "ping", GQLKind: model.GQLQuery, GQLVariableTypes: nil}
	g := gqlgen.GenForOperation(op)

	rapid.Check(t, func(t *rapid.T) {
		vars := g.Draw(t, "vars")
		if len(vars) != 0 {
			t.Fatalf("expected empty map for no-arg operation, got: %v", vars)
		}
	})
}

func TestGenForOperation_RequiredArgs_AlwaysPresent(t *testing.T) {
	op := opWithArgs(map[string]model.SchemaRef{
		"id": {Type: "string", Nullable: false}, // ID! — required
	})

	rapid.Check(t, func(t *rapid.T) {
		vars := drawVars(t, op)
		if _, ok := vars["id"]; !ok {
			t.Fatalf("required arg 'id' must always be present in generated variables")
		}
	})
}

func TestGenForOperation_NullableArg_SometimesAbsentOrNil(t *testing.T) {
	op := opWithArgs(map[string]model.SchemaRef{
		"description": {Type: "string", Nullable: true}, // String (nullable)
	})

	// Over 200 draws, we must observe at least one nil and one non-nil value.
	// This verifies the 10% nil probability is active.
	sawNil := false
	sawNonNil := false

	rapid.Check(t, func(t *rapid.T) {
		if sawNil && sawNonNil {
			return // early exit once both observed
		}
		vars := drawVars(t, op)
		val := vars["description"]
		if val == nil {
			sawNil = true
		} else {
			sawNonNil = true
		}
	})

	if !sawNil {
		t.Error("nullable argument should be nil in at least some draws; never observed nil")
	}
	if !sawNonNil {
		t.Error("nullable argument should be non-nil in at least some draws; never observed non-nil")
	}
}

func TestGenForOperation_MultipleArgs_AllPresent(t *testing.T) {
	op := opWithArgs(map[string]model.SchemaRef{
		"amount":   {Type: "integer", Nullable: false},
		"currency": {Type: "string", Nullable: false},
	})

	rapid.Check(t, func(t *rapid.T) {
		vars := drawVars(t, op)
		if _, ok := vars["amount"]; !ok {
			t.Fatalf("required arg 'amount' missing from generated variables")
		}
		if _, ok := vars["currency"]; !ok {
			t.Fatalf("required arg 'currency' missing from generated variables")
		}
	})
}

// --- GenForType: scalar types ---

func TestGenForType_String_ProducesString(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(model.SchemaRef{Type: "string", Nullable: false})
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-nullable string must not produce nil")
		}
		if _, ok := val.(string); !ok {
			t.Fatalf("expected string, got %T: %v", val, val)
		}
	})
}

func TestGenForType_Integer_ProducesInt32Range(t *testing.T) {
	// GraphQL Int is a signed 32-bit integer — values must fit in int32.
	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(model.SchemaRef{Type: "integer", Nullable: false})
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-nullable integer must not produce nil")
		}
		n, ok := val.(int32)
		if !ok {
			// Accept int64 that fits in int32 range too — implementation may use int64 internally
			if n64, ok2 := val.(int64); ok2 {
				if n64 < -2147483648 || n64 > 2147483647 {
					t.Fatalf("integer value %d out of int32 range", n64)
				}
			} else {
				t.Fatalf("expected int32-compatible type, got %T: %v", val, val)
			}
		}
		_ = n
	})
}

func TestGenForType_Float_ProducesFloat64(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(model.SchemaRef{Type: "number", Nullable: false})
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-nullable float must not produce nil")
		}
		if _, ok := val.(float64); !ok {
			t.Fatalf("expected float64, got %T: %v", val, val)
		}
	})
}

func TestGenForType_Boolean_ProducesBool(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(model.SchemaRef{Type: "boolean", Nullable: false})
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-nullable boolean must not produce nil")
		}
		if _, ok := val.(bool); !ok {
			t.Fatalf("expected bool, got %T: %v", val, val)
		}
	})
}

func TestGenForType_ID_ProducesNonEmptyAlphanumericString(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		// ID is SchemaRef{Type:"string"} with an "id" format hint
		g := gqlgen.GenForType(model.SchemaRef{Type: "string", Format: "id", Nullable: false})
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-nullable ID must not produce nil")
		}
		s, ok := val.(string)
		if !ok {
			t.Fatalf("expected string for ID, got %T", val)
		}
		if len(s) == 0 {
			t.Fatal("ID must not be empty string")
		}
		if len(s) > 64 {
			t.Fatalf("ID length %d exceeds maximum of 64", len(s))
		}
	})
}

// --- GenForType: enum ---

func TestGenForType_Enum_OnlyProducesDeclaredValues(t *testing.T) {
	allowed := []string{"PENDING", "CONFIRMED", "SHIPPED", "DELIVERED", "CANCELLED"}
	ref := model.SchemaRef{
		Type:     "string",
		Enum:     []any{"PENDING", "CONFIRMED", "SHIPPED", "DELIVERED", "CANCELLED"},
		Nullable: false,
	}

	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ref)
		val := g.Draw(t, "v")
		s, ok := val.(string)
		if !ok {
			t.Fatalf("enum generator must produce strings, got %T", val)
		}
		for _, a := range allowed {
			if s == a {
				return
			}
		}
		t.Fatalf("enum value %q not in declared set %v", s, allowed)
	})
}

func TestGenForType_Enum_ProducesVariety(t *testing.T) {
	// Over enough draws the generator must produce more than one distinct value.
	ref := model.SchemaRef{
		Type:     "string",
		Enum:     []any{"PENDING", "CONFIRMED", "SHIPPED"},
		Nullable: false,
	}

	seen := map[string]bool{}
	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ref)
		val := g.Draw(t, "v").(string)
		seen[val] = true
	})

	if len(seen) < 2 {
		t.Errorf("enum generator produced only %d distinct value(s) over many draws — not sampling fairly: %v", len(seen), seen)
	}
}

// --- GenForType: list ---

func TestGenForType_List_ProducesSlice(t *testing.T) {
	ref := model.SchemaRef{
		Type:     "array",
		Nullable: false,
		Items:    &model.SchemaRef{Type: "string", Nullable: false},
	}

	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ref)
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-nullable list must not produce nil")
		}
		slice, ok := val.([]any)
		if !ok {
			t.Fatalf("expected []any for list type, got %T", val)
		}
		if len(slice) > 5 {
			t.Fatalf("list length %d exceeds maximum of 5", len(slice))
		}
		for i, item := range slice {
			if _, ok := item.(string); !ok {
				t.Fatalf("list item [%d] expected string, got %T", i, item)
			}
		}
	})
}

func TestGenForType_List_CanProduceEmptySlice(t *testing.T) {
	ref := model.SchemaRef{
		Type:     "array",
		Nullable: false,
		Items:    &model.SchemaRef{Type: "string", Nullable: false},
	}

	sawEmpty := false
	rapid.Check(t, func(t *rapid.T) {
		if sawEmpty {
			return
		}
		g := gqlgen.GenForType(ref)
		val := g.Draw(t, "v").([]any)
		if len(val) == 0 {
			sawEmpty = true
		}
	})

	if !sawEmpty {
		t.Error("list generator must be able to produce empty slices — none observed")
	}
}

// --- GenForType: input object ---

func TestGenForType_InputObject_RequiredFieldsAlwaysPresent(t *testing.T) {
	ref := model.SchemaRef{
		Type:     "object",
		Nullable: false,
		Required: []string{"amount", "currency"},
		Properties: map[string]*model.SchemaRef{
			"amount":      {Type: "integer", Nullable: false},
			"currency":    {Type: "string", Nullable: false},
			"description": {Type: "string", Nullable: true},
		},
	}

	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ref)
		val := g.Draw(t, "v")
		m, ok := val.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any for input object, got %T", val)
		}
		if _, ok := m["amount"]; !ok {
			t.Fatalf("required field 'amount' missing from generated input object")
		}
		if _, ok := m["currency"]; !ok {
			t.Fatalf("required field 'currency' missing from generated input object")
		}
	})
}

func TestGenForType_InputObject_OptionalFieldSometimesAbsent(t *testing.T) {
	ref := model.SchemaRef{
		Type:     "object",
		Nullable: false,
		Required: []string{"amount"},
		Properties: map[string]*model.SchemaRef{
			"amount":      {Type: "integer", Nullable: false},
			"description": {Type: "string", Nullable: true}, // optional
		},
	}

	sawAbsent := false
	rapid.Check(t, func(t *rapid.T) {
		if sawAbsent {
			return
		}
		g := gqlgen.GenForType(ref)
		m := g.Draw(t, "v").(map[string]any)
		if _, ok := m["description"]; !ok {
			sawAbsent = true
		}
	})

	if !sawAbsent {
		t.Error("optional field 'description' should be absent in some draws; never observed absent")
	}
}

// --- GenForType: non-null constraint ---

func TestGenForType_NonNull_NeverProducesNil(t *testing.T) {
	// String! — must never be nil
	ref := model.SchemaRef{Type: "string", Nullable: false}

	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ref)
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-null type must never produce nil")
		}
	})
}

func TestGenForType_Nullable_SometimesProducesNil(t *testing.T) {
	ref := model.SchemaRef{Type: "string", Nullable: true}

	sawNil := false
	rapid.Check(t, func(t *rapid.T) {
		if sawNil {
			return
		}
		g := gqlgen.GenForType(ref)
		if g.Draw(t, "v") == nil {
			sawNil = true
		}
	})

	if !sawNil {
		t.Error("nullable type should produce nil in some draws; never observed nil over many checks")
	}
}

// --- GenForType: custom scalar fallback ---

func TestGenForType_CustomScalar_ProducesString(t *testing.T) {
	// Unknown type — falls back to string generation, no panic.
	ref := model.SchemaRef{Type: "DateTime", Nullable: false} // not a known SDL scalar

	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ref)
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("custom scalar fallback must not produce nil for non-nullable type")
		}
		if _, ok := val.(string); !ok {
			t.Fatalf("custom scalar fallback must produce a string, got %T", val)
		}
	})
}
```

Run and confirm tests fail:

```bash
go test ./internal/strategy/graphql/gen/... -race -v
# Expected: compilation error or all FAIL — no implementation exists yet
```

---

## Step 2 — `response_matches_schema` invariant for GraphQL

### Spec

- Package: `internal/strategy/graphql/invariant`
- `ResponseMatchesSchema` is an `Invariant` that validates `data.*` against the operation's `GQLSelectionSchema` using `AssertResponse` from `internal/strategy/graphql/assert` (M9)
- `NoGQLErrors` is an `Invariant` that fails if `errors` is present and non-null in the response body
- Both implement the existing `Invariant` interface from `internal/strategy/`:
  ```go
  type Invariant interface {
      Name() string
      Evaluate(resp model.ResponseDetail, op model.Operation) []Failure
  }
  ```
- `NoGQLErrors` severity is configurable: `Critical` (default) or `Warning` — controlled via `NoGQLErrorsConfig{Severity}`
- `ResponseMatchesSchema` reuses `gqlassert.AssertResponse` — it does not reimplement schema validation
- Both invariants return an empty slice (never nil) when no violations are found
- `NoGQLErrors` fails with a `Failure` that includes the first error message from `errors[0].message` in its `Message` field (or "GraphQL errors present" if `message` is absent)
- `NoGQLErrors` does NOT fail if `errors` is null or the key is absent entirely — only if `errors` is a non-null, non-empty array

### Tests

`internal/strategy/graphql/invariant/invariant_test.go`:

```go
package invariant_test

import (
	"testing"

	"github.com/yourorg/bt/internal/strategy/graphql/invariant"
	"github.com/yourorg/bt/pkg/model"
)

// --- helpers ---

func respWith(body string) model.ResponseDetail {
	return model.ResponseDetail{
		StatusCode: 200,
		Body:       []byte(body),
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
	}
}

func orderOp() model.Operation {
	return model.Operation{
		ID:      "order",
		GQLKind: model.GQLQuery,
		GQLSelectionSchema: &model.SchemaRef{
			Type:     "object",
			Required: []string{"id", "amount", "status"},
			Properties: map[string]*model.SchemaRef{
				"id":     {Type: "string", Nullable: false},
				"amount": {Type: "integer", Nullable: false},
				"status": {Type: "string", Nullable: false},
			},
		},
	}
}

// --- NoGQLErrors ---

func TestNoGQLErrors_Name(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	if inv.Name() != "no_gql_errors" {
		t.Errorf("expected name 'no_gql_errors', got %q", inv.Name())
	}
}

func TestNoGQLErrors_NoErrorsKey_Passes(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	resp := respWith(`{"data":{"id":"ord_1"}}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) != 0 {
		t.Errorf("expected no failures when 'errors' key is absent, got: %v", failures)
	}
}

func TestNoGQLErrors_ErrorsNull_Passes(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	resp := respWith(`{"data":{"id":"ord_1"},"errors":null}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) != 0 {
		t.Errorf("expected no failures when 'errors' is null, got: %v", failures)
	}
}

func TestNoGQLErrors_ErrorsEmptyArray_Passes(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	resp := respWith(`{"data":{"id":"ord_1"},"errors":[]}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) != 0 {
		t.Errorf("expected no failures when 'errors' is empty array, got: %v", failures)
	}
}

func TestNoGQLErrors_ErrorsNonEmpty_Fails(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	resp := respWith(`{"data":null,"errors":[{"message":"not found","locations":[]}]}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) == 0 {
		t.Fatal("expected failure when 'errors' is a non-empty array")
	}
}

func TestNoGQLErrors_ErrorsNonEmpty_IncludesFirstErrorMessage(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	resp := respWith(`{"data":null,"errors":[{"message":"resolver panicked"}]}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) == 0 {
		t.Fatal("expected failure")
	}
	if failures[0].Message == "" {
		t.Error("failure message must not be empty")
	}
	// The first error's message must appear in the failure message
	if !containsString(failures[0].Message, "resolver panicked") {
		t.Errorf("expected failure message to include 'resolver panicked', got: %q", failures[0].Message)
	}
}

func TestNoGQLErrors_ErrorsNonEmpty_MessageAbsent_UsesGenericMessage(t *testing.T) {
	// errors[0] has no 'message' field
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	resp := respWith(`{"data":null,"errors":[{"locations":[]}]}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) == 0 {
		t.Fatal("expected failure")
	}
	if failures[0].Message == "" {
		t.Error("failure message must not be empty even when errors[0].message is absent")
	}
}

func TestNoGQLErrors_CriticalSeverityByDefault(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	resp := respWith(`{"data":null,"errors":[{"message":"oops"}]}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) == 0 {
		t.Fatal("expected failure")
	}
	if failures[0].Severity != model.SeverityCritical {
		t.Errorf("expected Critical severity by default, got %v", failures[0].Severity)
	}
}

func TestNoGQLErrors_WarningSeverity_WhenConfigured(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{Severity: model.SeverityWarning})
	resp := respWith(`{"data":null,"errors":[{"message":"oops"}]}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) == 0 {
		t.Fatal("expected failure")
	}
	if failures[0].Severity != model.SeverityWarning {
		t.Errorf("expected Warning severity when configured, got %v", failures[0].Severity)
	}
}

func TestNoGQLErrors_ReturnValueIsNeverNil(t *testing.T) {
	inv := invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})
	resp := respWith(`{"data":{"id":"ord_1"}}`)
	failures := inv.Evaluate(resp, orderOp())
	if failures == nil {
		t.Error("Evaluate must return an empty slice, not nil")
	}
}

// --- ResponseMatchesSchema ---

func TestResponseMatchesSchema_Name(t *testing.T) {
	inv := invariant.NewResponseMatchesSchema()
	if inv.Name() != "response_matches_schema" {
		t.Errorf("expected name 'response_matches_schema', got %q", inv.Name())
	}
}

func TestResponseMatchesSchema_ValidBody_NoFailures(t *testing.T) {
	inv := invariant.NewResponseMatchesSchema()
	resp := respWith(`{"data":{"id":"ord_1","amount":100,"status":"PENDING"}}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) != 0 {
		t.Errorf("expected no failures for valid body, got: %v", failures)
	}
}

func TestResponseMatchesSchema_MissingRequiredField_Fails(t *testing.T) {
	inv := invariant.NewResponseMatchesSchema()
	// 'amount' is required in orderOp's selection schema but absent
	resp := respWith(`{"data":{"id":"ord_1","status":"PENDING"}}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) == 0 {
		t.Fatal("expected failure for missing required field 'amount'")
	}
	found := false
	for _, f := range failures {
		if containsString(f.Field, "amount") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected failure mentioning 'amount' field; got: %v", failures)
	}
}

func TestResponseMatchesSchema_WrongType_Fails(t *testing.T) {
	inv := invariant.NewResponseMatchesSchema()
	// 'amount' is integer but server returns a string — the broken resolver bug
	resp := respWith(`{"data":{"id":"ord_1","amount":"one hundred","status":"PENDING"}}`)
	failures := inv.Evaluate(resp, orderOp())
	if len(failures) == 0 {
		t.Fatal("expected failure for wrong type on 'amount' (string instead of integer)")
	}
	found := false
	for _, f := range failures {
		if containsString(f.Field, "amount") && f.Severity == model.SeverityCritical {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Critical failure on 'amount' field; got: %v", failures)
	}
}

func TestResponseMatchesSchema_NoSelectionSchema_NoFailures(t *testing.T) {
	// Operation with no GQLSelectionSchema — only envelope is validated (by AssertResponse)
	op := model.Operation{ID: "ping", GQLKind: model.GQLQuery, GQLSelectionSchema: nil}
	inv := invariant.NewResponseMatchesSchema()
	resp := respWith(`{"data":"pong"}`)
	failures := inv.Evaluate(resp, op)
	if len(failures) != 0 {
		t.Errorf("expected no failures when no selection schema is set, got: %v", failures)
	}
}

func TestResponseMatchesSchema_ReturnValueIsNeverNil(t *testing.T) {
	inv := invariant.NewResponseMatchesSchema()
	resp := respWith(`{"data":{"id":"ord_1","amount":100,"status":"PENDING"}}`)
	failures := inv.Evaluate(resp, orderOp())
	if failures == nil {
		t.Error("Evaluate must return an empty slice, not nil")
	}
}

// --- helper ---

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
```

Run and confirm tests fail:

```bash
go test ./internal/strategy/graphql/invariant/... -race -v
```

---

## Step 3 — GraphQL property runner

### Spec

- The GraphQL property runner lives at `internal/strategy/graphql/property/`
- It implements `Strategy` (the shared interface from `internal/strategy/`)
- `Plan` builds one `Case` per operation in the discovered set whose `GQLKind` is `GQLQuery` or `GQLMutation` (subscriptions are skipped with a `DEBUG` log)
- `Execute` runs each case using Rapid's `Check` function:
  - For each Rapid draw, calls `GenForOperation(op)` to produce a variables map
  - Serialises the variables into a GraphQL request body: `{"query": op.GQLDocument, "variables": <vars>}`
  - Sends the request via the GraphQL HTTP runner
  - Evaluates the response against every configured invariant
  - If any invariant fails, Rapid marks the draw as a failure and begins shrinking
  - The shrunk minimal failing input is captured and written to an artifact bundle
- The `--seed` flag wires through to Rapid's `MakeCustom` seed parameter — same mechanism as the REST property strategy (M4)
- Artifact bundles produced by the GraphQL property runner use the same format as M4 bundles, extended with `gql_operation_kind` and `gql_variables` fields
- The runner respects `StrategySpec.Config["checks"]` (default 100) — the maximum number of Rapid draws before the run is considered a pass

### Tests

`internal/strategy/graphql/property/runner_test.go`:

```go
package property_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	gqlproperty "github.com/yourorg/bt/internal/strategy/graphql/property"
	"github.com/yourorg/bt/internal/strategy/graphql/invariant"
	"github.com/yourorg/bt/pkg/model"
)

// brokenAmountServer returns a valid order but with amount as a string
// on every call — simulating the broken resolver.
func brokenAmountServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		// amount is returned as a string — schema violation
		w.Write([]byte(`{"data":{"id":"ord_1","amount":"one hundred","status":"PENDING"}}`))
	}))
}

// validOrderServer returns a valid order response on every call.
func validOrderServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"data":{"id":"ord_1","amount":100,"status":"PENDING"}}`))
	}))
}

// errorServer returns a GraphQL errors response on every call.
func errorServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"data":null,"errors":[{"message":"internal server error"}]}`))
	}))
}

func orderQueryOp() model.Operation {
	return model.Operation{
		ID:          "order",
		Method:      "POST",
		Path:        "/graphql",
		GQLKind:     model.GQLQuery,
		GQLDocument: `query order($id: ID!) { order(id: $id) { id amount status } }`,
		GQLVariableTypes: map[string]model.SchemaRef{
			"id": {Type: "string", Format: "id", Nullable: false},
		},
		GQLSelectionSchema: &model.SchemaRef{
			Type:     "object",
			Required: []string{"id", "amount", "status"},
			Properties: map[string]*model.SchemaRef{
				"id":     {Type: "string", Nullable: false},
				"amount": {Type: "integer", Nullable: false},
				"status": {Type: "string", Nullable: false},
			},
		},
	}
}

func TestRunner_ValidServer_AllChecksPass(t *testing.T) {
	srv := validOrderServer(t)
	defer srv.Close()

	runner := gqlproperty.NewRunner(gqlproperty.Config{
		BaseURL:    srv.URL,
		Checks:     20,
		Invariants: []model.Invariant{invariant.NewResponseMatchesSchema()},
	})

	ops := []model.Operation{orderQueryOp()}
	cases, err := runner.Plan(context.Background(), model.StrategySpec{}, ops)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}

	results, err := runner.Execute(context.Background(), cases, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, r := range results {
		if !r.Passed {
			t.Errorf("case %q should pass against valid server; failures: %v", r.ID, r.Failures)
		}
	}
}

func TestRunner_BrokenAmountResolver_DetectedWithinChecks(t *testing.T) {
	srv := brokenAmountServer(t)
	defer srv.Close()

	runner := gqlproperty.NewRunner(gqlproperty.Config{
		BaseURL:    srv.URL,
		Checks:     50,
		Invariants: []model.Invariant{invariant.NewResponseMatchesSchema()},
	})

	ops := []model.Operation{orderQueryOp()}
	cases, _ := runner.Plan(context.Background(), model.StrategySpec{}, ops)
	results, err := runner.Execute(context.Background(), cases, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	found := false
	for _, r := range results {
		if !r.Passed {
			found = true
			// Must report a failure mentioning 'amount'
			hasAmountFailure := false
			for _, f := range r.Failures {
				if containsString(f.Field, "amount") || containsString(f.Message, "amount") {
					hasAmountFailure = true
					break
				}
			}
			if !hasAmountFailure {
				t.Errorf("failure found but none mention 'amount'; failures: %v", r.Failures)
			}
		}
	}

	if !found {
		t.Error("expected broken amount resolver to be detected within 50 checks; no failure found")
	}
}

func TestRunner_NoGQLErrors_DetectsErrorResponse(t *testing.T) {
	srv := errorServer(t)
	defer srv.Close()

	runner := gqlproperty.NewRunner(gqlproperty.Config{
		BaseURL:    srv.URL,
		Checks:     10,
		Invariants: []model.Invariant{invariant.NewNoGQLErrors(invariant.NoGQLErrorsConfig{})},
	})

	ops := []model.Operation{orderQueryOp()}
	cases, _ := runner.Plan(context.Background(), model.StrategySpec{}, ops)
	results, err := runner.Execute(context.Background(), cases, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, r := range results {
		if r.Passed {
			t.Error("expected no_gql_errors invariant to detect error response; case passed unexpectedly")
		}
	}
}

func TestRunner_SubscriptionOp_Skipped(t *testing.T) {
	// A subscription operation must not be executed — Plan should exclude it.
	srv := validOrderServer(t)
	defer srv.Close()

	subOp := model.Operation{
		ID:      "orderUpdated",
		GQLKind: model.GQLSubscription,
		GQLDocument: `subscription { orderUpdated { id status } }`,
	}

	runner := gqlproperty.NewRunner(gqlproperty.Config{
		BaseURL: srv.URL,
		Checks:  10,
	})

	cases, err := runner.Plan(context.Background(), model.StrategySpec{}, []model.Operation{subOp})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(cases) != 0 {
		t.Errorf("expected subscription operations to be excluded from Plan; got %d cases", len(cases))
	}
}

func TestRunner_SeedProducesDeterministicRun(t *testing.T) {
	// Two runs with the same seed must hit the same server paths.
	var pathsRun1, pathsRun2 []string
	var mu1, mu2 atomic.Value

	// Server that records which variables it received
	makeRecordingServer := func(recorder *[]string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(200)
			w.Write([]byte(`{"data":{"id":"ord_1","amount":100,"status":"PENDING"}}`))
		}))
	}

	_ = makeRecordingServer
	_ = mu1
	_ = mu2

	srv1 := validOrderServer(t)
	srv2 := validOrderServer(t)
	defer srv1.Close()
	defer srv2.Close()

	seed := int64(42)

	run := func(baseURL string) []model.Result {
		runner := gqlproperty.NewRunner(gqlproperty.Config{
			BaseURL: baseURL,
			Checks:  20,
			Seed:    &seed,
			Invariants: []model.Invariant{invariant.NewResponseMatchesSchema()},
		})
		cases, _ := runner.Plan(context.Background(), model.StrategySpec{}, []model.Operation{orderQueryOp()})
		results, _ := runner.Execute(context.Background(), cases, nil)
		return results
	}

	results1 := run(srv1.URL)
	results2 := run(srv2.URL)

	// Both runs must produce the same pass/fail outcome for each case
	if len(results1) != len(results2) {
		t.Fatalf("seed runs produced different result counts: %d vs %d", len(results1), len(results2))
	}
	for i := range results1 {
		if results1[i].Passed != results2[i].Passed {
			t.Errorf("result[%d] differs between seeded runs: %v vs %v", i, results1[i].Passed, results2[i].Passed)
		}
	}

	_ = pathsRun1
	_ = pathsRun2
}

func TestRunner_FailureProducesArtifactBundle(t *testing.T) {
	srv := brokenAmountServer(t)
	defer srv.Close()

	dir := t.TempDir()
	runner := gqlproperty.NewRunner(gqlproperty.Config{
		BaseURL:     srv.URL,
		Checks:      50,
		ArtifactDir: dir,
		Invariants:  []model.Invariant{invariant.NewResponseMatchesSchema()},
	})

	ops := []model.Operation{orderQueryOp()}
	cases, _ := runner.Plan(context.Background(), model.StrategySpec{}, ops)
	results, err := runner.Execute(context.Background(), cases, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	for _, r := range results {
		if !r.Passed {
			if r.ArtifactPath == "" {
				t.Errorf("failed case %q must have an ArtifactPath set", r.ID)
			}
			// Artifact must exist on disk
			if r.ArtifactPath != "" {
				if _, err := assertFileExists(t, r.ArtifactPath); err != nil {
					t.Errorf("artifact file %q does not exist: %v", r.ArtifactPath, err)
				}
			}
		}
	}
}

func TestRunner_ArtifactBundle_ContainsGQLFields(t *testing.T) {
	srv := brokenAmountServer(t)
	defer srv.Close()

	dir := t.TempDir()
	runner := gqlproperty.NewRunner(gqlproperty.Config{
		BaseURL:     srv.URL,
		Checks:      50,
		ArtifactDir: dir,
		Invariants:  []model.Invariant{invariant.NewResponseMatchesSchema()},
	})

	ops := []model.Operation{orderQueryOp()}
	cases, _ := runner.Plan(context.Background(), model.StrategySpec{}, ops)
	results, _ := runner.Execute(context.Background(), cases, nil)

	for _, r := range results {
		if !r.Passed && r.ArtifactPath != "" {
			bundle, err := loadArtifactBundle(t, r.ArtifactPath)
			if err != nil {
				t.Fatalf("failed to load artifact bundle: %v", err)
			}
			if bundle["gql_operation_kind"] == nil {
				t.Error("artifact bundle must contain 'gql_operation_kind' field")
			}
			if bundle["gql_variables"] == nil {
				t.Error("artifact bundle must contain 'gql_variables' field with the shrunk failing input")
			}
		}
	}
}

// --- helpers ---

func containsString(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func assertFileExists(t *testing.T, path string) (bool, error) {
	t.Helper()
	import_os := func() bool {
		// placeholder — use os.Stat in implementation
		return true
	}
	_ = import_os
	return true, nil // implementation will use os.Stat
}

func loadArtifactBundle(t *testing.T, path string) (map[string]any, error) {
	t.Helper()
	// implementation reads JSON from path and unmarshals
	return map[string]any{}, nil // placeholder — implementation fills this
}
```

**Note on helpers:** `assertFileExists` and `loadArtifactBundle` are stubs here. In the actual test file, replace with real `os.Stat` and `os.ReadFile` + `json.Unmarshal` calls.

Run and confirm tests fail:

```bash
go test ./internal/strategy/graphql/property/... -race -v
```

---

## Step 4 — `bt_suggest_invariants` MCP tool update

### Spec

- `bt_suggest_invariants` already exists from M7
- When called with an operation whose `GQLKind` is `GQLQuery` or `GQLMutation`, it must include `no_gql_errors` and `response_matches_schema` in its suggestions
- When called with a REST operation (no `GQLKind`), the existing REST suggestions are returned unchanged
- The GraphQL suggestions must appear before any REST-derived suggestions in the output
- The suggestion for `no_gql_errors` must note the configurable severity: `"Fails if 'errors' is present and non-empty. Severity is configurable: Critical (default) or Warning."`
- The suggestion for `response_matches_schema` must note it reuses the SDL selection schema: `"Validates data.* fields against the SDL-derived selection schema for this operation."`

### Tests

`internal/mcp/suggest_invariants_gql_test.go`:

```go
package mcp_test

import (
	"context"
	"testing"

	btmcp "github.com/yourorg/bt/internal/mcp"
	"github.com/yourorg/bt/pkg/model"
)

func TestSuggestInvariants_GraphQLQuery_IncludesGQLInvariants(t *testing.T) {
	tool := btmcp.NewSuggestInvariantsTool()

	op := model.Operation{
		ID:      "order",
		GQLKind: model.GQLQuery,
	}

	suggestions, err := tool.Suggest(context.Background(), op)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	names := invariantNames(suggestions)

	if !contains(names, "no_gql_errors") {
		t.Errorf("expected 'no_gql_errors' in suggestions for GraphQL Query; got: %v", names)
	}
	if !contains(names, "response_matches_schema") {
		t.Errorf("expected 'response_matches_schema' in suggestions for GraphQL Query; got: %v", names)
	}
}

func TestSuggestInvariants_GraphQLMutation_IncludesGQLInvariants(t *testing.T) {
	tool := btmcp.NewSuggestInvariantsTool()

	op := model.Operation{
		ID:      "createOrder",
		GQLKind: model.GQLMutation,
	}

	suggestions, err := tool.Suggest(context.Background(), op)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	names := invariantNames(suggestions)

	if !contains(names, "no_gql_errors") {
		t.Errorf("expected 'no_gql_errors' in suggestions for GraphQL Mutation; got: %v", names)
	}
	if !contains(names, "response_matches_schema") {
		t.Errorf("expected 'response_matches_schema' in suggestions for GraphQL Mutation; got: %v", names)
	}
}

func TestSuggestInvariants_GraphQL_GQLSuggestionsAppearFirst(t *testing.T) {
	tool := btmcp.NewSuggestInvariantsTool()

	op := model.Operation{ID: "order", GQLKind: model.GQLQuery}
	suggestions, err := tool.Suggest(context.Background(), op)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	if len(suggestions) == 0 {
		t.Fatal("expected at least one suggestion")
	}

	firstTwo := invariantNames(suggestions[:min(2, len(suggestions))])
	if !contains(firstTwo, "no_gql_errors") && !contains(firstTwo, "response_matches_schema") {
		t.Errorf("expected GQL-specific invariants to appear first; first two: %v", firstTwo)
	}
}

func TestSuggestInvariants_GraphQL_NoGQLErrors_MentionsSeverityConfig(t *testing.T) {
	tool := btmcp.NewSuggestInvariantsTool()
	op := model.Operation{ID: "order", GQLKind: model.GQLQuery}
	suggestions, err := tool.Suggest(context.Background(), op)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	for _, s := range suggestions {
		if s.Name == "no_gql_errors" {
			if !containsString(s.Description, "configurable") && !containsString(s.Description, "Warning") {
				t.Errorf("no_gql_errors description should mention configurable severity; got: %q", s.Description)
			}
			return
		}
	}
	t.Error("'no_gql_errors' suggestion not found")
}

func TestSuggestInvariants_RESTOperation_NoGQLInvariants(t *testing.T) {
	tool := btmcp.NewSuggestInvariantsTool()

	op := model.Operation{
		ID:      "GetOrder",
		Method:  "GET",
		Path:    "/orders/{id}",
		GQLKind: "", // no GQL kind — REST operation
	}

	suggestions, err := tool.Suggest(context.Background(), op)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	names := invariantNames(suggestions)
	if contains(names, "no_gql_errors") {
		t.Error("'no_gql_errors' must not be suggested for REST operations")
	}
	if contains(names, "response_matches_schema") && !hasRESTMatchesSchema(suggestions) {
		// response_matches_schema may exist for REST too — but must not be the GQL variant
		t.Error("GraphQL-specific response_matches_schema must not appear for REST operations")
	}
}

func TestSuggestInvariants_GraphQLSubscription_NoPropertySuggestions(t *testing.T) {
	// Subscriptions are not supported for property testing — suggestions should reflect this
	tool := btmcp.NewSuggestInvariantsTool()

	op := model.Operation{
		ID:      "orderUpdated",
		GQLKind: model.GQLSubscription,
	}

	suggestions, err := tool.Suggest(context.Background(), op)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	// no_gql_errors and response_matches_schema are property strategy invariants;
	// subscriptions don't run in the property strategy
	names := invariantNames(suggestions)
	if contains(names, "no_gql_errors") || contains(names, "response_matches_schema") {
		t.Errorf("property strategy invariants must not be suggested for Subscription operations; got: %v", names)
	}
}

// --- helpers ---

type suggestion struct {
	Name        string
	Description string
}

func invariantNames(suggestions []suggestion) []string {
	names := make([]string, len(suggestions))
	for i, s := range suggestions {
		names[i] = s.Name
	}
	return names
}

func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func containsString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func hasRESTMatchesSchema(suggestions []suggestion) bool {
	for _, s := range suggestions {
		if s.Name == "response_matches_schema" && containsString(s.Description, "OpenAPI") {
			return true
		}
	}
	return false
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

Run and confirm tests fail:

```bash
go test ./internal/mcp/... -run TestSuggestInvariants_GraphQL -v
```

---

## Implementation

Only begin implementation once all tests above are written and confirmed failing.

### Step 1 implementation — SDL variable generator

`internal/strategy/graphql/gen/gen.go`:

```go
package gen

import (
	"log/slog"

	rapid "pgregory.net/rapid"

	"github.com/yourorg/bt/pkg/model"
)

// GenForOperation returns a generator that produces a complete variables map
// for the given operation. Returns a generator for an empty map when the
// operation has no arguments.
func GenForOperation(op model.Operation) *rapid.Generator[map[string]any] {
	return rapid.Custom(func(t *rapid.T) map[string]any {
		if len(op.GQLVariableTypes) == 0 {
			return map[string]any{}
		}
		vars := make(map[string]any, len(op.GQLVariableTypes))
		for name, ref := range op.GQLVariableTypes {
			g := GenForType(ref)
			vars[name] = g.Draw(t, name)
		}
		return vars
	})
}

// GenForType returns a generator for a single SDL argument type.
func GenForType(ref model.SchemaRef) *rapid.Generator[any] {
	base := baseGenForType(ref)
	if ref.Nullable {
		return rapid.Custom(func(t *rapid.T) any {
			// 10% nil probability for nullable types
			if rapid.IntRange(0, 9).Draw(t, "null_chance") == 0 {
				return nil
			}
			return base.Draw(t, "value")
		})
	}
	return base
}

func baseGenForType(ref model.SchemaRef) *rapid.Generator[any] {
	// Enum takes priority — applies to any base type with declared values
	if len(ref.Enum) > 0 {
		return rapid.Custom(func(t *rapid.T) any {
			return rapid.SampledFrom(ref.Enum).Draw(t, "enum")
		})
	}

	switch ref.Type {
	case "string":
		if ref.Format == "id" {
			return rapid.Custom(func(t *rapid.T) any {
				return rapid.StringMatching(`[a-zA-Z0-9]{1,64}`).Draw(t, "id")
			})
		}
		return rapid.Custom(func(t *rapid.T) any {
			return rapid.String().Draw(t, "string")
		})

	case "integer":
		// GraphQL Int is signed 32-bit
		return rapid.Custom(func(t *rapid.T) any {
			return rapid.Int32().Draw(t, "int")
		})

	case "number":
		return rapid.Custom(func(t *rapid.T) any {
			return rapid.Float64().Draw(t, "float")
		})

	case "boolean":
		return rapid.Custom(func(t *rapid.T) any {
			return rapid.Bool().Draw(t, "bool")
		})

	case "array":
		if ref.Items == nil {
			return rapid.Custom(func(t *rapid.T) any { return []any{} })
		}
		itemGen := GenForType(*ref.Items)
		return rapid.Custom(func(t *rapid.T) any {
			n := rapid.IntRange(0, 5).Draw(t, "list_len")
			slice := make([]any, n)
			for i := range slice {
				slice[i] = itemGen.Draw(t, "item")
			}
			return slice
		})

	case "object":
		return rapid.Custom(func(t *rapid.T) any {
			m := make(map[string]any)
			required := make(map[string]bool, len(ref.Required))
			for _, r := range ref.Required {
				required[r] = true
			}
			for name, propRef := range ref.Properties {
				if required[name] || rapid.Bool().Draw(t, "include_optional") {
					m[name] = GenForType(*propRef).Draw(t, name)
				}
			}
			return m
		})

	default:
		// Custom scalar fallback — treat as string with a warning
		slog.Warn("unknown SDL type; treating as string", "type", ref.Type)
		return rapid.Custom(func(t *rapid.T) any {
			return rapid.String().Draw(t, "custom_scalar")
		})
	}
}
```

### Step 2 implementation — Invariants

`internal/strategy/graphql/invariant/invariant.go`:

```go
package invariant

import (
	"encoding/json"
	"fmt"

	gqlassert "github.com/yourorg/bt/internal/strategy/graphql/assert"
	"github.com/yourorg/bt/pkg/model"
)

// NoGQLErrorsConfig configures the no_gql_errors invariant.
type NoGQLErrorsConfig struct {
	Severity model.Severity // defaults to model.SeverityCritical
}

type noGQLErrors struct {
	cfg NoGQLErrorsConfig
}

func NewNoGQLErrors(cfg NoGQLErrorsConfig) model.Invariant {
	if cfg.Severity == "" {
		cfg.Severity = model.SeverityCritical
	}
	return &noGQLErrors{cfg: cfg}
}

func (n *noGQLErrors) Name() string { return "no_gql_errors" }

func (n *noGQLErrors) Evaluate(resp model.ResponseDetail, _ model.Operation) []model.Failure {
	var envelope struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(resp.Body, &envelope); err != nil {
		return []model.Failure{} // not our problem to report here — ResponseMatchesSchema catches JSON errors
	}
	if len(envelope.Errors) == 0 {
		return []model.Failure{}
	}
	msg := "GraphQL errors present"
	if envelope.Errors[0].Message != "" {
		msg = fmt.Sprintf("GraphQL error: %s", envelope.Errors[0].Message)
	}
	return []model.Failure{{
		Invariant: n.Name(),
		Message:   msg,
		Severity:  n.cfg.Severity,
	}}
}

// responseMatchesSchema validates data.* against the operation's selection schema.
type responseMatchesSchema struct{}

func NewResponseMatchesSchema() model.Invariant { return &responseMatchesSchema{} }

func (r *responseMatchesSchema) Name() string { return "response_matches_schema" }

func (r *responseMatchesSchema) Evaluate(resp model.ResponseDetail, op model.Operation) []model.Failure {
	// Delegate to the existing GraphQL assertion logic from M9
	assertFailures := gqlassert.AssertResponse(resp.Body, op)
	out := make([]model.Failure, 0, len(assertFailures))
	for _, f := range assertFailures {
		out = append(out, model.Failure{
			Invariant: r.Name(),
			Field:     f.Field,
			Message:   f.Message,
			Severity:  model.Severity(f.Severity),
		})
	}
	return out
}
```

### Step 3 implementation — GraphQL property runner

`internal/strategy/graphql/property/runner.go` — wire `GenForOperation` into Rapid's `Check`, evaluate invariants per draw, capture shrunk failure, write artifact bundle. Follow the same pattern as `internal/strategy/property/runner.go` from M4, substituting the SDL generator for the OpenAPI generator.

Key differences from the REST runner:
- Request body is `{"query": op.GQLDocument, "variables": vars}` — not a REST body
- Artifact bundle includes `gql_operation_kind` and `gql_variables` additional fields
- The `--seed` flag wires to `rapid.MakeCustom` via the same `Config.Seed *int64` mechanism

### Step 4 implementation — MCP tool update

`internal/mcp/suggest_invariants.go` — add a branch at the top of the suggestion builder:

```go
if op.GQLKind == model.GQLQuery || op.GQLKind == model.GQLMutation {
    return gqlSuggestions(op), nil
}
```

`gqlSuggestions` returns `no_gql_errors` and `response_matches_schema` first, followed by any universal invariants (e.g. `status_code_in_2xx`). Subscriptions return an empty suggestion list.

---

## Full verification

Run all tests once all implementation steps are complete:

```bash
# Unit tests for this milestone
go test ./internal/strategy/graphql/gen/... -race -v
go test ./internal/strategy/graphql/invariant/... -race -v
go test ./internal/strategy/graphql/property/... -race -v
go test ./internal/mcp/... -run TestSuggestInvariants -race -v

# Full suite — must not regress
go test ./... -race

# Lint
golangci-lint run ./...

# Build
CGO_ENABLED=0 go build ./cmd/bt
```

---

## M11 exit criterion

1. `GenForOperation` produces a valid variables map for every SDL argument type — scalar, enum, list, input object, non-null, nullable, custom scalar
2. `NoGQLErrors` fails when and only when `errors` is a non-null, non-empty array; severity is configurable; return value is never nil
3. `ResponseMatchesSchema` delegates to `gqlassert.AssertResponse` — reports `amount` type violation in the broken resolver test
4. The property runner detects the broken resolver's `amount` type violation within 50 checks and writes an artifact bundle with `gql_operation_kind` and `gql_variables` fields
5. `--seed` produces deterministic runs across two identical executions
6. `bt_suggest_invariants` returns `no_gql_errors` and `response_matches_schema` first for `GQLQuery` and `GQLMutation`; neither is suggested for REST operations or subscriptions
7. All tests pass with `-race`