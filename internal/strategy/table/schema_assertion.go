package table

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jayimbery/bt/internal/strategy/property/validate"
	"github.com/jayimbery/bt/pkg/model"
)

// SchemaAssertion evaluates a JSON response body against a JSON Schema-shaped model.SchemaRef.
type SchemaAssertion struct {
	schema *model.SchemaRef
}

// NewSchemaAssertion constructs an assertion for the given schema.
func NewSchemaAssertion(schema *model.SchemaRef) *SchemaAssertion {
	return &SchemaAssertion{schema: schema}
}

// Evaluate validates body against the schema and returns all violations (never nil).
func (sa *SchemaAssertion) Evaluate(body []byte) []model.SchemaViolation {
	if sa == nil || sa.schema == nil {
		return []model.SchemaViolation{}
	}
	return EvaluateSchema(sa.schema, body)
}

// EvaluateSchema is the package-level evaluator used by SchemaAssertion and table Execute.
func EvaluateSchema(schema *model.SchemaRef, body []byte) []model.SchemaViolation {
	schema = effectiveSchemaRef(schema)
	if schema == nil {
		return []model.SchemaViolation{}
	}

	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) == 0 {
		if schemaExpectsDocument(schema) {
			t := schema.Type
			if t == "" {
				t = "object"
			}
			return []model.SchemaViolation{{
				Field:        "$",
				Message:      "response body is empty; schema expects " + t,
				ExpectedType: t,
				ActualType:   "empty",
				Severity:     model.SeverityCritical,
			}}
		}
	}

	var out []model.SchemaViolation
	for _, v := range validate.ValidateResponse(body, schema) {
		out = append(out, validateViolationToModel(v))
	}
	// Post-process invalid JSON message to match product wording.
	for i := range out {
		if out[i].Field == "$" && strings.Contains(out[i].Message, "invalid JSON") {
			out[i].Message = "response body is not valid JSON: " + out[i].ActualType
		}
	}

	var root any
	if err := json.Unmarshal(body, &root); err == nil {
		appendAdditionalPropertyWarningsJSONPath("$", root, schema, &out)
	}
	return out
}

func schemaExpectsDocument(s *model.SchemaRef) bool {
	if s == nil {
		return false
	}
	return s.Type == "object" || s.Type == "array" || (s.Type == "" && len(s.Properties) > 0)
}

func effectiveSchemaRef(s *model.SchemaRef) *model.SchemaRef {
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

func validateViolationToModel(v validate.SchemaViolation) model.SchemaViolation {
	return model.SchemaViolation{
		Field:        v.Path,
		ExpectedType: v.Expected,
		ActualType:   simplifyGotType(v.Got),
		Message:      v.Message,
		Severity:     model.SeverityCritical,
	}
}

func simplifyGotType(got string) string {
	// e.g. "not-a-number (string)" -> take trailing parenthesised type if present
	if i := strings.LastIndex(got, "("); i >= 0 && strings.HasSuffix(got, ")") {
		inner := got[i+1 : len(got)-1]
		if inner != "" && !strings.Contains(inner, " ") {
			return inner
		}
	}
	return got
}

func joinJSONPath(prefix, name string) string {
	if prefix == "$" {
		return "$." + name
	}
	return prefix + "." + name
}

func appendAdditionalPropertyWarningsJSONPath(jsonPathPrefix string, v any, schema *model.SchemaRef, out *[]model.SchemaViolation) {
	if schema == nil {
		return
	}
	schema = effectiveSchemaRef(schema)
	switch schema.Type {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return
		}
		disallow := schema.AdditionalProperties != nil && !*schema.AdditionalProperties
		if disallow && schema.Properties != nil {
			for k := range obj {
				if _, declared := schema.Properties[k]; !declared {
					*out = append(*out, model.SchemaViolation{
						Field:        joinJSONPath(jsonPathPrefix, k),
						ExpectedType: "no additional properties",
						ActualType:   fmt.Sprintf("undeclared field %q present", k),
						Message:      fmt.Sprintf("property %q is not allowed by schema", k),
						Severity:     model.SeverityWarning,
					})
				}
			}
		}
		if schema.Properties == nil {
			return
		}
		for k, prop := range schema.Properties {
			if prop == nil {
				continue
			}
			child, ok := obj[k]
			if !ok {
				continue
			}
			appendAdditionalPropertyWarningsJSONPath(joinJSONPath(jsonPathPrefix, k), child, prop, out)
		}
	case "array":
		arr, ok := v.([]any)
		if !ok || schema.Items == nil {
			return
		}
		for i, el := range arr {
			childPath := fmt.Sprintf("%s[%d]", jsonPathPrefix, i)
			appendAdditionalPropertyWarningsJSONPath(childPath, el, schema.Items, out)
		}
	}
}

func gqlDataJSONPath(validatePath string) string {
	if validatePath == "$" {
		return "$.data"
	}
	return "$.data" + strings.TrimPrefix(validatePath, "$")
}

// EvaluateGraphQLDataSchema validates the GraphQL `data` value from a full GraphQL HTTP response body.
// Violation Field paths are rooted at $.data...
func EvaluateGraphQLDataSchema(schema *model.SchemaRef, body []byte) []model.SchemaViolation {
	if schema == nil {
		return []model.SchemaViolation{}
	}
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return []model.SchemaViolation{{
			Field:        "$",
			Message:      "response body is not valid JSON: " + err.Error(),
			ExpectedType: "valid JSON document",
			ActualType:   err.Error(),
			Severity:     model.SeverityCritical,
		}}
	}
	data := root["data"]
	raw, err := json.Marshal(data)
	if err != nil {
		return []model.SchemaViolation{{
			Field:        "$.data",
			Message:      "gql_data_schema: marshal data: " + err.Error(),
			Severity:     model.SeverityCritical,
			ExpectedType: "JSON-serializable value",
			ActualType:   err.Error(),
		}}
	}
	base := EvaluateSchema(schema, raw)
	for i := range base {
		base[i].Field = gqlDataJSONPath(base[i].Field)
	}
	return base
}

func schemaViolationsToFailures(vv []model.SchemaViolation) []model.Failure {
	out := make([]model.Failure, 0, len(vv))
	for _, v := range vv {
		if v.Severity != model.SeverityCritical {
			continue
		}
		out = append(out, model.Failure{
			Invariant: model.InvariantResponseMatchesSchema,
			Message:   v.Message,
			Path:      v.Field,
			Expected:  v.ExpectedType,
			Actual:    v.ActualType,
		})
	}
	return out
}
