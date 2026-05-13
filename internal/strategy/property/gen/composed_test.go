package gen_test

import (
	"fmt"
	"testing"

	rapid "pgregory.net/rapid"

	"github.com/jayimbery/bt/internal/strategy/property/gen"
	"github.com/jayimbery/bt/pkg/model"
)

func stringSchema() *model.SchemaRef {
	return &model.SchemaRef{Type: "string"}
}

func integerSchema() *model.SchemaRef {
	return &model.SchemaRef{Type: "integer"}
}

func stringEnumSchema(values []string) *model.SchemaRef {
	enum := make([]any, len(values))
	for i, v := range values {
		enum[i] = v
	}
	return &model.SchemaRef{Type: "string", Enum: enum}
}

func requiredStringSchema() *model.SchemaRef {
	return &model.SchemaRef{Type: "string", Nullable: false}
}

func optionalStringSchema() *model.SchemaRef {
	return &model.SchemaRef{Type: "string", Nullable: true}
}

func repeat(values []string, times int) []any {
	result := make([]any, 0, len(values)*times)
	for i := 0; i < times; i++ {
		for _, v := range values {
			result = append(result, v)
		}
	}
	return result
}

func uniqueCount(values []any) int {
	seen := map[any]bool{}
	for _, v := range values {
		seen[v] = true
	}
	return len(seen)
}

func TestTraceDistributionChoicesLength(t *testing.T) {
	ch := gen.TraceDistributionChoices(map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05})
	if len(ch) != 1000 {
		t.Fatalf("want 1000 slots, got %d", len(ch))
	}
}

func TestComposedGenerator_NoTraceData_GeneratesArbitraryStrings(t *testing.T) {
	g := gen.NewComposedGenerator(stringSchema(), nil)
	rapid.Check(t, func(rt *rapid.T) {
		vals := make([]any, 100)
		for i := range vals {
			vals[i] = g.Draw(rt, fmt.Sprintf("v%d", i))
		}
		if uniqueCount(vals) <= 5 {
			t.Fatalf("expected varied strings with no trace data, got only %d unique values", uniqueCount(vals))
		}
	})
}

func TestComposedGenerator_SufficientDistribution_DrawsFromDistribution(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:         "string",
		Samples:      repeat([]string{"GBP", "USD", "EUR"}, 10),
		Distribution: map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05},
	}
	g := gen.NewComposedGenerator(stringSchema(), traceArg)
	rapid.Check(t, func(rt *rapid.T) {
		s := g.Draw(rt, "v").(string)
		if s != "GBP" && s != "USD" && s != "EUR" {
			t.Fatalf("expected only distribution values, got %q", s)
		}
	})
}

func TestComposedGenerator_InsufficientSamples_FallsBackToSchema(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:    "string",
		Samples: []any{"GBP", "USD"},
	}
	g := gen.NewComposedGenerator(stringSchema(), traceArg)
	rapid.Check(t, func(rt *rapid.T) {
		vals := make([]any, 100)
		for i := range vals {
			vals[i] = g.Draw(rt, fmt.Sprintf("v%d", i))
		}
		if uniqueCount(vals) <= 2 {
			t.Fatalf("insufficient samples should fall back to schema generator producing variety; got only %d unique values", uniqueCount(vals))
		}
	})
}

func TestComposedGenerator_AllObservedValuesInvalid_FallsBackToSchema(t *testing.T) {
	enumSchema := stringEnumSchema([]string{"GBP", "USD"})
	traceArg := &model.ArgumentProfile{
		Type:         "string",
		Samples:      repeat([]string{"INVALID"}, 30),
		Distribution: map[string]float64{"INVALID": 1.0},
	}
	g := gen.NewComposedGenerator(enumSchema, traceArg)
	rapid.Check(t, func(rt *rapid.T) {
		for i := 0; i < 100; i++ {
			v := g.Draw(rt, fmt.Sprintf("v%d", i))
			s := v.(string)
			if s != "GBP" && s != "USD" {
				t.Fatalf("expected only schema-valid values after invalid trace drop; got %q", s)
			}
		}
	})
}

func TestComposedGenerator_NumericRange_AppliedToIntegerSchema(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:  "integer",
		Range: &model.Range{Min: 10, Max: 500},
	}
	g := gen.NewComposedGenerator(integerSchema(), traceArg)

	rapid.Check(t, func(rt *rapid.T) {
		val := g.Draw(rt, "v")
		var n int64
		switch v := val.(type) {
		case int64:
			n = v
		case int:
			n = int64(v)
		case float64:
			n = int64(v)
		default:
			t.Fatalf("expected integer-compatible type, got %T", val)
		}
		if n < 10 || n > 500 {
			t.Fatalf("value %d outside observed range [10, 500]", n)
		}
	})
}

func TestComposedGenerator_NullRateFromTrace_NonNullableFieldNeverNil(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:     "string",
		NullRate: 0.5,
		Samples:  repeat([]string{"GBP"}, 30),
	}
	g := gen.NewComposedGenerator(requiredStringSchema(), traceArg)
	rapid.Check(t, func(rt *rapid.T) {
		for i := 0; i < 200; i++ {
			v := g.Draw(rt, fmt.Sprintf("v%d", i))
			if v == nil {
				t.Fatal("non-nullable field must never be nil, even with high trace null rate")
			}
		}
	})
}

func TestComposedGenerator_AlwaysPresent_RemovesOptionalWrapper(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:          "string",
		AlwaysPresent: true,
		Samples:       repeat([]string{"note"}, 30),
		Distribution:  map[string]float64{"note": 1.0},
	}
	g := gen.NewComposedGenerator(optionalStringSchema(), traceArg)
	rapid.Check(t, func(rt *rapid.T) {
		for i := 0; i < 200; i++ {
			v := g.Draw(rt, fmt.Sprintf("v%d", i))
			if v == nil {
				t.Fatal("AlwaysPresent=true must prevent nil generation even on optional/nullable field")
			}
		}
	})
}
