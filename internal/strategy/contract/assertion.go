package contract

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jimbery/bt/internal/strategy/property/validate"
	"github.com/jimbery/bt/pkg/model"
)

// EvaluateBody checks a decoded JSON object body against a SchemaRef and returns
// every violation found. The slice is never nil (empty means no violations).
func EvaluateBody(body map[string]any, schema *model.SchemaRef) []ContractViolation {
	raw, err := json.Marshal(body)
	if err != nil {
		return []ContractViolation{{
			Field:    "body",
			Expected: "JSON-serializable object",
			Actual:   err.Error(),
			Severity: Critical,
		}}
	}
	return EvaluateJSON(raw, schema)
}

// EvaluateJSON validates raw JSON bytes against schema (same rules as EvaluateBody).
func EvaluateJSON(body []byte, schema *model.SchemaRef) []ContractViolation {
	if schema == nil {
		return nil
	}
	var violations []ContractViolation
	for _, v := range validate.ValidateResponse(body, schema) {
		violations = append(violations, ContractViolation{
			Field:    displayPath(v.Path),
			Expected: v.Expected,
			Actual:   v.Got,
			Severity: Critical,
		})
	}
	var root any
	if err := json.Unmarshal(body, &root); err == nil {
		warningsAdditionalProperties("", root, schema, &violations)
	}
	return violations
}

func displayPath(p string) string {
	if p == "$" || p == "" {
		return p
	}
	if strings.HasPrefix(p, "$.") {
		return p[2:]
	}
	if strings.HasPrefix(p, "$[") {
		return p[1:]
	}
	return p
}

func warningsAdditionalProperties(prefix string, v any, schema *model.SchemaRef, out *[]ContractViolation) {
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
					*out = append(*out, ContractViolation{
						Field:    joinField(prefix, k),
						Expected: "no additional properties",
						Actual:   fmt.Sprintf("undeclared field %q present", k),
						Severity: Warning,
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
			warningsAdditionalProperties(joinField(prefix, k), child, prop, out)
		}
	case "array":
		arr, ok := v.([]any)
		if !ok || schema.Items == nil {
			return
		}
		for i, el := range arr {
			p := fmt.Sprintf("%s[%d]", prefix, i)
			if prefix == "" {
				p = fmt.Sprintf("[%d]", i)
			}
			warningsAdditionalProperties(p, el, schema.Items, out)
		}
	}
}

func joinField(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
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
