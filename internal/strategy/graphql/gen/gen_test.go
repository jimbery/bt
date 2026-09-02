package gen_test

import (
	"testing"

	rapid "pgregory.net/rapid"

	gqlgen "github.com/jimbery/bt/internal/strategy/graphql/gen"
	"github.com/jimbery/bt/pkg/model"
)

func ptr(s model.SchemaRef) *model.SchemaRef {
	return &s
}

func opWithArgs(args map[string]*model.SchemaRef) model.Operation {
	return model.Operation{
		ID:               "TestOp",
		GQLKind:          model.GQLMutation,
		GQLVariableTypes: args,
	}
}

func drawVars(t *rapid.T, op model.Operation) map[string]any {
	t.Helper()
	g := gqlgen.GenForOperation(op)
	return g.Draw(t, "vars")
}

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
	op := opWithArgs(map[string]*model.SchemaRef{
		"id": ptr(model.SchemaRef{Type: "string", Format: "id", Nullable: false}),
	})

	rapid.Check(t, func(t *rapid.T) {
		vars := drawVars(t, op)
		if _, ok := vars["id"]; !ok {
			t.Fatalf("required arg 'id' must always be present in generated variables")
		}
	})
}

func TestGenForOperation_NullableArg_SometimesAbsentOrNil(t *testing.T) {
	op := opWithArgs(map[string]*model.SchemaRef{
		"description": ptr(model.SchemaRef{Type: "string", Nullable: true}),
	})

	sawNil := false
	sawNonNil := false

	rapid.Check(t, func(t *rapid.T) {
		if sawNil && sawNonNil {
			return
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
	op := opWithArgs(map[string]*model.SchemaRef{
		"amount":   ptr(model.SchemaRef{Type: "integer", Nullable: false}),
		"currency": ptr(model.SchemaRef{Type: "string", Nullable: false}),
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

func TestGenForType_String_ProducesString(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ptr(model.SchemaRef{Type: "string", Nullable: false}))
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-nullable string must not produce nil")
		}
		if _, ok := val.(string); !ok {
			t.Fatalf("expected string, got %T: %v", val, val)
		}
	})
}

func TestGenForType_Integer_ProducesNonNegativeInt32(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ptr(model.SchemaRef{Type: "integer", Nullable: false}))
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-nullable integer must not produce nil")
		}
		switch v := val.(type) {
		case int32:
			if v < 0 {
				t.Fatalf("expected non-negative int32, got %d", v)
			}
		case int64:
			if v < 0 || v > 2147483647 {
				t.Fatalf("integer value %d out of expected non-negative int32 range", v)
			}
		case int:
			if v < 0 || int64(v) > 2147483647 {
				t.Fatalf("integer value %d out of expected non-negative int32 range", v)
			}
		default:
			t.Fatalf("expected int32-compatible type, got %T: %v", val, val)
		}
	})
}

func TestGenForType_Float_ProducesFloat64(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ptr(model.SchemaRef{Type: "number", Nullable: false}))
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
		g := gqlgen.GenForType(ptr(model.SchemaRef{Type: "boolean", Nullable: false}))
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
		g := gqlgen.GenForType(ptr(model.SchemaRef{Type: "string", Format: "id", Nullable: false}))
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

func TestGenForType_Enum_OnlyProducesDeclaredValues(t *testing.T) {
	allowed := []string{"PENDING", "CONFIRMED", "SHIPPED", "DELIVERED", "CANCELLED"}
	ref := ptr(model.SchemaRef{
		Type:     "string",
		Enum:     []any{"PENDING", "CONFIRMED", "SHIPPED", "DELIVERED", "CANCELLED"},
		Nullable: false,
	})

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
	ref := ptr(model.SchemaRef{
		Type:     "string",
		Enum:     []any{"PENDING", "CONFIRMED", "SHIPPED"},
		Nullable: false,
	})

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

func TestGenForType_List_ProducesSlice(t *testing.T) {
	ref := ptr(model.SchemaRef{
		Type:     "array",
		Nullable: false,
		Items:    ptr(model.SchemaRef{Type: "string", Nullable: false}),
	})

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
	ref := ptr(model.SchemaRef{
		Type:     "array",
		Nullable: false,
		Items:    ptr(model.SchemaRef{Type: "string", Nullable: false}),
	})

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

func TestGenForType_InputObject_RequiredFieldsAlwaysPresent(t *testing.T) {
	ref := ptr(model.SchemaRef{
		Type:     "object",
		Nullable: false,
		Required: []string{"amount", "currency"},
		Properties: map[string]*model.SchemaRef{
			"amount":      ptr(model.SchemaRef{Type: "integer", Nullable: false}),
			"currency":    ptr(model.SchemaRef{Type: "string", Nullable: false}),
			"description": ptr(model.SchemaRef{Type: "string", Nullable: true}),
		},
	})

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
	ref := ptr(model.SchemaRef{
		Type:     "object",
		Nullable: false,
		Required: []string{"amount"},
		Properties: map[string]*model.SchemaRef{
			"amount":      ptr(model.SchemaRef{Type: "integer", Nullable: false}),
			"description": ptr(model.SchemaRef{Type: "string", Nullable: true}),
		},
	})

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

func TestGenForType_NonNull_NeverProducesNil(t *testing.T) {
	ref := ptr(model.SchemaRef{Type: "string", Nullable: false})

	rapid.Check(t, func(t *rapid.T) {
		g := gqlgen.GenForType(ref)
		val := g.Draw(t, "v")
		if val == nil {
			t.Fatalf("non-null type must never produce nil")
		}
	})
}

func TestGenForType_Nullable_SometimesProducesNil(t *testing.T) {
	ref := ptr(model.SchemaRef{Type: "string", Nullable: true})

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

func TestGenForType_CustomScalar_ProducesString(t *testing.T) {
	ref := ptr(model.SchemaRef{Type: "DateTime", Nullable: false})

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
