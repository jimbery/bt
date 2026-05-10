// Package validate checks JSON response bodies against normalised schema refs.
package validate

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/jayimbery/bt/pkg/model"
)

// SchemaViolation records a single schema mismatch.
type SchemaViolation struct {
	Path     string `json:"path"`
	Message  string `json:"message"`
	Expected string `json:"expected"`
	Got      string `json:"got"`
}

// ValidateResponse unmarshals body as JSON and validates it against schema.
// Malformed JSON returns a single violation at path "$".
func ValidateResponse(body []byte, schema *model.SchemaRef) []SchemaViolation {
	if schema == nil {
		return nil
	}
	schema = effectiveSchema(schema)
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return []SchemaViolation{{
			Path:     "$",
			Message:  "invalid JSON",
			Expected: "valid JSON document",
			Got:      err.Error(),
		}}
	}
	return validateValue("$", root, schema)
}

func effectiveSchema(s *model.SchemaRef) *model.SchemaRef {
	if s == nil {
		return nil
	}
	if s.Type == "" && len(s.Properties) > 0 {
		c := *s
		c.Type = "object"
		return &c
	}
	return s
}

func validateValue(path string, v any, schema *model.SchemaRef) []SchemaViolation {
	if schema == nil {
		return nil
	}
	if v == nil {
		if schema.Nullable || schema.Type == "null" {
			return nil
		}
		return []SchemaViolation{{
			Path:     path,
			Message:  "unexpected null",
			Expected: "non-null value",
			Got:      "null",
		}}
	}
	var out []SchemaViolation

	if len(schema.OneOf) > 0 {
		return validateOneOf(path, v, schema.OneOf)
	}
	if len(schema.AnyOf) > 0 {
		return validateAnyOf(path, v, schema.AnyOf)
	}

	switch schema.Type {
	case "object":
		return validateObject(path, v, schema)
	case "array":
		return validateArray(path, v, schema)
	case "string":
		s, ok := v.(string)
		if !ok {
			out = append(out, typeViolation(path, "string", fmt.Sprintf("%T", v)))
			break
		}
		if len(schema.Enum) > 0 && !enumContains(schema.Enum, v) {
			out = append(out, SchemaViolation{Path: path, Message: "value not in enum", Expected: fmt.Sprintf("%v", schema.Enum), Got: fmt.Sprintf("%q", s)})
			break
		}
		if schema.MinLength != nil && len(s) < *schema.MinLength {
			out = append(out, SchemaViolation{Path: path, Message: "string too short", Expected: fmt.Sprintf("minLength %d", *schema.MinLength), Got: strconv.Itoa(len(s))})
		}
		if schema.MaxLength != nil && len(s) > *schema.MaxLength {
			out = append(out, SchemaViolation{Path: path, Message: "string too long", Expected: fmt.Sprintf("maxLength %d", *schema.MaxLength), Got: strconv.Itoa(len(s))})
		}
	case "integer":
		if !isJSONInteger(v) {
			out = append(out, typeViolation(path, "integer", fmt.Sprintf("%v (%T)", v, v)))
			break
		}
		if len(schema.Enum) > 0 && !enumContains(schema.Enum, v) {
			out = append(out, SchemaViolation{Path: path, Message: "value not in enum", Expected: fmt.Sprintf("%v", schema.Enum), Got: fmt.Sprintf("%v", v)})
			break
		}
		n, _ := asInt64(v)
		if schema.Minimum != nil && float64(n) < *schema.Minimum {
			out = append(out, SchemaViolation{Path: path, Message: "below minimum", Expected: fmt.Sprintf(">= %g", *schema.Minimum), Got: fmt.Sprintf("%d", n)})
		}
		if schema.Maximum != nil && float64(n) > *schema.Maximum {
			out = append(out, SchemaViolation{Path: path, Message: "above maximum", Expected: fmt.Sprintf("<= %g", *schema.Maximum), Got: fmt.Sprintf("%d", n)})
		}
	case "number":
		f, ok := toFloat64(v)
		if !ok {
			out = append(out, typeViolation(path, "number", fmt.Sprintf("%T", v)))
			break
		}
		if schema.Minimum != nil && f < *schema.Minimum {
			out = append(out, SchemaViolation{Path: path, Message: "below minimum", Expected: fmt.Sprintf(">= %g", *schema.Minimum), Got: fmt.Sprintf("%g", f)})
		}
		if schema.Maximum != nil && f > *schema.Maximum {
			out = append(out, SchemaViolation{Path: path, Message: "above maximum", Expected: fmt.Sprintf("<= %g", *schema.Maximum), Got: fmt.Sprintf("%g", f)})
		}
		if len(schema.Enum) > 0 && !enumContains(schema.Enum, v) {
			out = append(out, SchemaViolation{Path: path, Message: "value not in enum", Expected: fmt.Sprintf("%v", schema.Enum), Got: fmt.Sprintf("%v", v)})
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			out = append(out, typeViolation(path, "boolean", fmt.Sprintf("%T", v)))
		}
	case "null":
		out = append(out, SchemaViolation{Path: path, Message: "expected null", Expected: "null", Got: fmt.Sprintf("%v", v)})
	default:
		// Unknown composite: best-effort pass.
	}
	return out
}

func validateOneOf(path string, v any, options []*model.SchemaRef) []SchemaViolation {
	for _, opt := range options {
		if opt == nil {
			continue
		}
		if len(validateValue(path, v, opt)) == 0 {
			return nil
		}
	}
	return []SchemaViolation{{Path: path, Message: "value matches no oneOf branch", Expected: "oneOf", Got: fmt.Sprintf("%v", v)}}
}

func validateAnyOf(path string, v any, options []*model.SchemaRef) []SchemaViolation {
	for _, opt := range options {
		if opt == nil {
			continue
		}
		if len(validateValue(path, v, opt)) == 0 {
			return nil
		}
	}
	return []SchemaViolation{{Path: path, Message: "value matches no anyOf branch", Expected: "anyOf", Got: fmt.Sprintf("%v", v)}}
}

func validateObject(path string, v any, schema *model.SchemaRef) []SchemaViolation {
	obj, ok := v.(map[string]any)
	if !ok {
		return []SchemaViolation{typeViolation(path, "object", fmt.Sprintf("%T", v))}
	}
	var out []SchemaViolation
	for _, req := range schema.Required {
		if _, ok := obj[req]; !ok {
			out = append(out, SchemaViolation{
				Path:     path + "." + req,
				Message:  "missing required field",
				Expected: "present",
				Got:      "absent",
			})
		}
	}
	for k, val := range obj {
		propSchema, known := schema.Properties[k]
		if !known {
			continue // extra fields ignored (no additionalProperties:false in model)
		}
		if propSchema == nil {
			continue
		}
		childPath := path + "." + k
		out = append(out, validateValue(childPath, val, propSchema)...)
	}
	return out
}

func validateArray(path string, v any, schema *model.SchemaRef) []SchemaViolation {
	arr, ok := v.([]any)
	if !ok {
		return []SchemaViolation{typeViolation(path, "array", fmt.Sprintf("%T", v))}
	}
	var out []SchemaViolation
	if schema.MinItems != nil && len(arr) < *schema.MinItems {
		out = append(out, SchemaViolation{Path: path, Message: "array too short", Expected: fmt.Sprintf("minItems %d", *schema.MinItems), Got: strconv.Itoa(len(arr))})
	}
	if schema.MaxItems != nil && len(arr) > *schema.MaxItems {
		out = append(out, SchemaViolation{Path: path, Message: "array too long", Expected: fmt.Sprintf("maxItems %d", *schema.MaxItems), Got: strconv.Itoa(len(arr))})
	}
	if schema.Items == nil {
		return out
	}
	for i, el := range arr {
		cp := fmt.Sprintf("%s[%d]", path, i)
		if path == "$" {
			cp = fmt.Sprintf("$[%d]", i)
		}
		out = append(out, validateValue(cp, el, schema.Items)...)
	}
	return out
}

func typeViolation(path, want, gotType string) SchemaViolation {
	return SchemaViolation{Path: path, Message: "wrong type", Expected: want, Got: gotType}
}

func enumContains(enums []any, v any) bool {
	for _, e := range enums {
		if jsonEqual(e, v) {
			return true
		}
	}
	return false
}

func jsonEqual(a, b any) bool {
	// Normalise float64/int comparison for JSON numbers.
	fa, aok := toFloat64(a)
	fb, bok := toFloat64(b)
	if aok && bok {
		return math.Abs(fa-fb) < 1e-9
	}
	return fmt.Sprint(a) == fmt.Sprint(b)
}

func isJSONInteger(v any) bool {
	switch x := v.(type) {
	case int, int32, int64, json.Number:
		return true
	case float64:
		return x == math.Trunc(x) && !math.IsInf(x, 0) && !math.IsNaN(x)
	default:
		return false
	}
}

func asInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int64:
		return x, true
	case float64:
		return int64(x), true
	case json.Number:
		i, err := x.Int64()
		return i, err == nil
	default:
		return 0, false
	}
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
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
