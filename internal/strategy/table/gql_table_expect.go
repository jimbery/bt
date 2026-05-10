package table

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/jayimbery/bt/internal/strategy/property/validate"
	"github.com/jayimbery/bt/pkg/model"
)

func gqlTableExpectationFailures(body []byte, exp *model.CaseExpectation) []model.Failure {
	if exp == nil {
		return nil
	}
	if exp.GQLData == nil && exp.GQLNoErrors == nil && exp.GQLHasErrors == nil && exp.GQLDataSchema == nil {
		return nil
	}

	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return []model.Failure{{
			Invariant: model.InvariantGraphQLResponse,
			Message:   fmt.Sprintf("gql expectations: invalid JSON: %v", err),
			Path:      "body",
		}}
	}

	var out []model.Failure

	if exp.GQLNoErrors != nil && *exp.GQLNoErrors {
		if graphqlErrorsNonEmpty(root) {
			out = append(out, model.Failure{
				Invariant: model.InvariantGraphQLResponse,
				Message:   "expected no GraphQL errors",
				Path:      "errors",
			})
		}
	}
	if exp.GQLHasErrors != nil && *exp.GQLHasErrors {
		if !graphqlErrorsNonEmpty(root) {
			out = append(out, model.Failure{
				Invariant: model.InvariantGraphQLResponse,
				Message:   "expected GraphQL errors",
				Path:      "errors",
			})
		}
	}

	data := root["data"]
	if len(exp.GQLData) > 0 {
		dm, ok := data.(map[string]any)
		if !ok || !gqlPartialDataMatch(exp.GQLData, dm) {
			out = append(out, model.Failure{
				Invariant: model.InvariantGraphQLResponse,
				Message:   fmt.Sprintf("gql_data mismatch: want %#v", exp.GQLData),
				Path:      "data",
			})
		}
	}

	if exp.GQLDataSchema != nil {
		raw, err := json.Marshal(data)
		if err != nil {
			out = append(out, model.Failure{
				Invariant: model.InvariantGraphQLResponse,
				Message:   fmt.Sprintf("gql_data_schema: marshal data: %v", err),
				Path:      "data",
			})
		} else {
			for _, v := range validate.ValidateResponse(raw, exp.GQLDataSchema) {
				p := v.Path
				if p != "" && p != "body" && p != "$" && p[0] != '[' {
					p = "data." + p
				} else if p == "" || p == "$" {
					p = "data"
				}
				out = append(out, model.Failure{
					Invariant: model.InvariantResponseMatchesSchema,
					Message:   v.Message,
					Path:      p,
					Expected:  v.Expected,
					Actual:    v.Got,
				})
			}
		}
	}

	return out
}

func graphqlErrorsNonEmpty(root map[string]any) bool {
	e, ok := root["errors"]
	if !ok || e == nil {
		return false
	}
	arr, ok := e.([]any)
	return ok && len(arr) > 0
}

func gqlPartialDataMatch(want map[string]any, got map[string]any) bool {
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			return false
		}
		if !gqlValuesEqual(wv, gv) {
			return false
		}
	}
	return true
}

func gqlValuesEqual(want, got any) bool {
	if want == nil && got == nil {
		return true
	}
	if want == nil || got == nil {
		// YAML null vs missing JSON null
		if want == nil {
			return got == nil
		}
		return false
	}

	switch w := want.(type) {
	case map[string]any:
		gm, ok := got.(map[string]any)
		if !ok {
			return false
		}
		return gqlPartialDataMatch(w, gm)
	case []any:
		ga, ok := got.([]any)
		if !ok || len(w) != len(ga) {
			return false
		}
		for i := range w {
			if !gqlValuesEqual(w[i], ga[i]) {
				return false
			}
		}
		return true
	}

	return numericAwareEqual(want, got)
}

func numericAwareEqual(want, got any) bool {
	fw, wok := toFloat(want)
	fg, gok := toFloat(got)
	if wok && gok {
		return math.Abs(fw-fg) < 1e-9
	}
	return fmt.Sprint(want) == fmt.Sprint(got)
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
