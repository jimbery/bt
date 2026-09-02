// Package gen maps normalised OpenAPI schema refs to Rapid generators.
package gen

import (
	"math"

	"pgregory.net/rapid"

	"github.com/jimbery/bt/pkg/model"
)

const maxGenDepth = 5

// GenForSchema returns a Rapid generator that draws values compatible with the
// given JSON-schema-shaped model.SchemaRef. Nil or empty schema yields only nil.
func GenForSchema(s *model.SchemaRef) *rapid.Generator[any] {
	if s == nil {
		return rapid.Just[any](nil)
	}
	return genAtDepth(s, 0)
}

func genAtDepth(s *model.SchemaRef, depth int) *rapid.Generator[any] {
	if s == nil || depth > maxGenDepth {
		return rapid.Just[any](nil)
	}
	if len(s.Enum) > 0 {
		return EnumGen(s.Enum)
	}
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
		return rapid.Just[any](nil)
	}

	if s.Nullable {
		return nullableTenPercent(base)
	}
	return base
}

func nullableTenPercent(base *rapid.Generator[any]) *rapid.Generator[any] {
	return rapid.Custom(func(t *rapid.T) any {
		if rapid.IntRange(0, 99).Draw(t, "nullable") < 10 {
			return nil
		}
		return base.Draw(t, "value")
	})
}

func oneOfGen(options []*model.SchemaRef, depth int) *rapid.Generator[any] {
	gens := make([]*rapid.Generator[any], 0, len(options))
	for _, o := range options {
		if o == nil {
			continue
		}
		gens = append(gens, genAtDepth(o, depth+1))
	}
	if len(gens) == 0 {
		return rapid.Just[any](nil)
	}
	return rapid.OneOf(gens...)
}

func stringGen(s *model.SchemaRef) *rapid.Generator[any] {
	minR, maxR := 0, 32
	if s.MinLength != nil && *s.MinLength > minR {
		minR = *s.MinLength
	}
	if s.MaxLength != nil {
		maxR = *s.MaxLength
		if maxR < minR {
			maxR = minR
		}
	}
	// maxLen (bytes) bound for Rapid — keep generous for unicode.
	maxBytes := maxR * 4
	if maxBytes < 256 {
		maxBytes = 256
	}
	return rapid.Map(rapid.StringN(minR, maxR, maxBytes), func(v string) any { return v })
}

func intGen(s *model.SchemaRef) *rapid.Generator[any] {
	if s.Minimum == nil && s.Maximum == nil {
		return rapid.Map(rapid.Int64(), func(v int64) any { return v })
	}
	lo, hi := int64(math.MinInt64/2), int64(math.MaxInt64/2)
	if s.Minimum != nil {
		lo = int64(math.Ceil(*s.Minimum))
	}
	if s.Maximum != nil {
		hi = int64(math.Floor(*s.Maximum))
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	return rapid.Map(rapid.Int64Range(lo, hi), func(v int64) any { return v })
}

func numberGen(s *model.SchemaRef) *rapid.Generator[any] {
	if s.Minimum == nil && s.Maximum == nil {
		return rapid.Map(rapid.Float64(), func(v float64) any { return v })
	}
	lo, hi := -math.MaxFloat64/4, math.MaxFloat64/4
	if s.Minimum != nil {
		lo = *s.Minimum
	}
	if s.Maximum != nil {
		hi = *s.Maximum
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	return rapid.Map(rapid.Float64Range(lo, hi), func(v float64) any { return v })
}

func arrayGen(s *model.SchemaRef, depth int) *rapid.Generator[any] {
	if s.Items == nil {
		return rapid.Just[any]([]any{})
	}
	elem := genAtDepth(s.Items, depth+1)
	minLen, maxLen := 0, 8
	if s.MinItems != nil {
		minLen = *s.MinItems
	}
	if s.MaxItems != nil {
		maxLen = *s.MaxItems
		if maxLen < minLen {
			maxLen = minLen
		}
	}
	return rapid.Map(rapid.SliceOfN(elem, minLen, maxLen), func(sl []any) any { return sl })
}

func objectGen(s *model.SchemaRef, depth int) *rapid.Generator[any] {
	if len(s.Properties) == 0 {
		return rapid.Just[any](map[string]any{})
	}
	req := make(map[string]struct{}, len(s.Required))
	for _, k := range s.Required {
		req[k] = struct{}{}
	}
	return rapid.Custom(func(t *rapid.T) any {
		out := make(map[string]any, len(s.Properties))
		for name, prop := range s.Properties {
			if prop == nil {
				continue
			}
			if _, required := req[name]; required {
				out[name] = genAtDepth(prop, depth+1).Draw(t, name)
				continue
			}
			if rapid.Bool().Draw(t, "opt_"+name) {
				out[name] = genAtDepth(prop, depth+1).Draw(t, name)
			}
		}
		return out
	})
}

// EnumGen builds a generator that samples one of the enum values as drawn JSON types.
func EnumGen(values []any) *rapid.Generator[any] {
	if len(values) == 0 {
		return rapid.Just[any](nil)
	}
	return rapid.SampledFrom(values)
}
