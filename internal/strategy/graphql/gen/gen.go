// Package gen builds Rapid generators for GraphQL variable maps from SDL-derived SchemaRefs.
package gen

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"pgregory.net/rapid"

	"github.com/jayimbery/bt/pkg/model"
)

var unknownScalarLog sync.Map // map[string]struct{}

// GenForOperation returns a generator that produces a variables map for op.GQLVariableTypes.
// When the operation has no variable types, every draw returns an empty map.
func GenForOperation(op model.Operation) *rapid.Generator[map[string]any] {
	if len(op.GQLVariableTypes) == 0 {
		return rapid.Just(map[string]any{})
	}
	return rapid.Custom(func(t *rapid.T) map[string]any {
		names := make([]string, 0, len(op.GQLVariableTypes))
		for n := range op.GQLVariableTypes {
			names = append(names, n)
		}
		sort.Strings(names)
		vars := make(map[string]any, len(names))
		drew := false
		for _, name := range names {
			ref := op.GQLVariableTypes[name]
			if ref == nil {
				continue
			}
			vars[name] = GenForType(ref).Draw(t, name)
			drew = true
		}
		if !drew {
			_ = rapid.Bool().Draw(t, "gql_vars_fallback")
		}
		return vars
	})
}

// GenForType returns a Rapid generator for a single SDL-shaped SchemaRef.
func GenForType(ref *model.SchemaRef) *rapid.Generator[any] {
	if ref == nil {
		return rapid.Just[any](nil)
	}
	base := baseGenForType(ref)
	if ref.Nullable {
		return rapid.Custom(func(t *rapid.T) any {
			if rapid.IntRange(0, 9).Draw(t, "null_chance") == 0 {
				return nil
			}
			return base.Draw(t, "value")
		})
	}
	return base
}

func baseGenForType(ref *model.SchemaRef) *rapid.Generator[any] {
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
		rs := []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 ")
		return rapid.Custom(func(t *rapid.T) any {
			return rapid.StringOfN(rapid.RuneFrom(rs), 0, 48, 256).Draw(t, "string")
		})

	case "integer":
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
		itemGen := GenForType(ref.Items)
		return rapid.Custom(func(t *rapid.T) any {
			n := rapid.IntRange(0, 5).Draw(t, "list_len")
			slice := make([]any, n)
			for i := range slice {
				slice[i] = itemGen.Draw(t, fmt.Sprintf("item%d", i))
			}
			return slice
		})

	case "object":
		if len(ref.Properties) == 0 {
			return rapid.Custom(func(t *rapid.T) any {
				_ = rapid.Bool().Draw(t, "empty_obj")
				return map[string]any{}
			})
		}
		return rapid.Custom(func(t *rapid.T) any {
			m := make(map[string]any)
			req := make(map[string]struct{}, len(ref.Required))
			for _, r := range ref.Required {
				req[r] = struct{}{}
			}
			drew := false
			for name, propRef := range ref.Properties {
				if propRef == nil {
					continue
				}
				if _, required := req[name]; required {
					m[name] = GenForType(propRef).Draw(t, name)
					drew = true
					continue
				}
				if rapid.Bool().Draw(t, "opt_"+name) {
					m[name] = GenForType(propRef).Draw(t, name)
					drew = true
				}
			}
			if !drew {
				// Ensure the bitstream advances even if every property pointer was nil.
				_ = rapid.Bool().Draw(t, "object_fallback")
			}
			return m
		})

	default:
		if _, loaded := unknownScalarLog.LoadOrStore(ref.Type, struct{}{}); !loaded {
			slog.Warn("graphql gen: unknown SDL type; treating as string", "type", ref.Type)
		}
		return rapid.Custom(func(t *rapid.T) any {
			return rapid.StringOfN(rapid.RuneFrom([]rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")), 1, 32, 128).Draw(t, "custom_scalar")
		})
	}
}
