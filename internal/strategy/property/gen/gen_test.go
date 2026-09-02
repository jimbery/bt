package gen_test

import (
	"testing"

	"pgregory.net/rapid"

	"github.com/jimbery/bt/internal/strategy/property/gen"
	"github.com/jimbery/bt/pkg/model"
)

func TestGenForSchema_String(t *testing.T) {
	t.Parallel()
	g := gen.GenForSchema(&model.SchemaRef{Type: "string"})
	rapid.Check(t, func(rt *rapid.T) {
		v := g.Draw(rt, "v")
		if _, ok := v.(string); !ok {
			t.Fatalf("want string, got %T", v)
		}
	})
}

func TestGenForSchema_IntegerBounded(t *testing.T) {
	t.Parallel()
	min, max := 1.0, 3.0
	g := gen.GenForSchema(&model.SchemaRef{Type: "integer", Minimum: &min, Maximum: &max})
	rapid.Check(t, func(rt *rapid.T) {
		v := g.Draw(rt, "v").(int64)
		if v < 1 || v > 3 {
			t.Fatalf("out of range: %d", v)
		}
	})
}
