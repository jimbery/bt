package gen

import (
	"math"
	"math/rand/v2"
	"sort"

	"pgregory.net/rapid"

	"github.com/jimbery/bt/pkg/model"
)

// ComposedGenerator combines trace-derived distributions with schema constraints (M12 / ADR-009).
type ComposedGenerator struct {
	inner *rapid.Generator[any]
}

// NewComposedGenerator returns a generator for one JSON value (scalar or small object leaf)
// using trace statistics when available, otherwise the schema generator.
func NewComposedGenerator(schema *model.SchemaRef, trace *model.ArgumentProfile) *ComposedGenerator {
	return &ComposedGenerator{inner: buildComposed(schema, trace)}
}

// Draw draws one value using Rapid.
func (c *ComposedGenerator) Draw(t *rapid.T, label string) any {
	if c == nil || c.inner == nil {
		return nil
	}
	return c.inner.Draw(t, label)
}

func buildComposed(schema *model.SchemaRef, trace *model.ArgumentProfile) *rapid.Generator[any] {
	if schema == nil {
		return rapid.Just[any](nil)
	}
	base := GenForSchema(schema)

	if trace == nil {
		return base
	}

	// AlwaysPresent on nullable field: never emit nil.
	if trace.AlwaysPresent && schema.Nullable {
		base = nonNilGen(schema)
	}

	// Rule 1: normalised distribution with enough trace mass and schema-valid keys.
	if len(trace.Distribution) > 0 {
		valid := filterDistribution(schema, trace.Distribution)
		if len(valid) > 0 {
			return distributionGen(schema, valid, base)
		}
	}

	// Rule 2: numeric range from trace intersected with schema bounds.
	if trace.Range != nil && (schema.Type == "integer" || schema.Type == "number") {
		return rangedNumericGen(schema, trace.Range, base)
	}

	// Rule 3: schema-only (includes insufficient trace samples).
	return base
}

func nonNilGen(schema *model.SchemaRef) *rapid.Generator[any] {
	s2 := *schema
	s2.Nullable = false
	return GenForSchema(&s2)
}

func filterDistribution(schema *model.SchemaRef, dist map[string]float64) map[string]float64 {
	if len(dist) == 0 {
		return nil
	}
	if schema == nil || len(schema.Enum) == 0 {
		out := make(map[string]float64)
		for k, v := range dist {
			if v > 0 {
				out[k] = v
			}
		}
		return normaliseFloatMap(out)
	}
	allowed := make(map[string]struct{}, len(schema.Enum))
	for _, e := range schema.Enum {
		if s, ok := e.(string); ok {
			allowed[s] = struct{}{}
		}
	}
	out := make(map[string]float64)
	for k, v := range dist {
		if _, ok := allowed[k]; ok && v > 0 {
			out[k] = v
		}
	}
	return normaliseFloatMap(out)
}

func normaliseFloatMap(m map[string]float64) map[string]float64 {
	sum := 0.0
	for _, v := range m {
		sum += v
	}
	if sum <= 0 {
		return nil
	}
	out := make(map[string]float64, len(m))
	for k, v := range m {
		out[k] = v / sum
	}
	return out
}

func distributionGen(_ *model.SchemaRef, dist map[string]float64, fallback *rapid.Generator[any]) *rapid.Generator[any] {
	ch := TraceDistributionChoices(dist)
	if len(ch) == 0 {
		return fallback
	}
	// rapid.SampledFrom over a large multiset is biased under Rapid's internal indexing;
	// uniform index into the 1000-slot multiset matches the intended trace frequencies.
	return rapid.Custom(func(t *rapid.T) any {
		_ = rapid.Bool().Draw(t, "dist_rng")
		return ch[rand.IntN(len(ch))]
	})
}

// TraceDistributionChoices expands a normalised distribution to a 1000-slot multiset (for tests / diagnostics).
func TraceDistributionChoices(dist map[string]float64) []string {
	keys := make([]string, 0, len(dist))
	for k := range dist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	weights := make([]int, len(keys))
	sum := 0
	for i, k := range keys {
		w := int(math.Round(dist[k] * 1000))
		if w < 0 {
			w = 0
		}
		weights[i] = w
		sum += w
	}
	if sum == 0 {
		return nil
	}
	if sum != 1000 {
		weights[len(weights)-1] += 1000 - sum
	}
	var ch []string
	for i, k := range keys {
		for j := 0; j < weights[i]; j++ {
			ch = append(ch, k)
		}
	}
	return ch
}

func rangedNumericGen(schema *model.SchemaRef, r *model.Range, fallback *rapid.Generator[any]) *rapid.Generator[any] {
	lo, hi := r.Min, r.Max
	if schema.Minimum != nil {
		lo = math.Max(lo, *schema.Minimum)
	}
	if schema.Maximum != nil {
		hi = math.Min(hi, *schema.Maximum)
	}
	if lo > hi {
		return fallback
	}
	switch schema.Type {
	case "integer":
		ilo, ihi := int64(math.Ceil(lo)), int64(math.Floor(hi))
		if ilo > ihi {
			return fallback
		}
		return rapid.Map(rapid.Int64Range(ilo, ihi), func(v int64) any { return v })
	case "number":
		return rapid.Map(rapid.Float64Range(lo, hi), func(v float64) any { return v })
	default:
		return fallback
	}
}
