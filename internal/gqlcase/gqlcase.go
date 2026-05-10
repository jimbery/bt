// Package gqlcase builds HTTP case inputs for GraphQL operations discovered via the graphql adapter.
package gqlcase

import (
	"fmt"
	"strings"

	"github.com/jayimbery/bt/internal/strategy/property/gen"
	"github.com/jayimbery/bt/pkg/model"
)

// IsGraphQLOperation reports whether op carries a GraphQL operation document.
func IsGraphQLOperation(op model.Operation) bool {
	return strings.TrimSpace(op.GQLDocument) != ""
}

// FillPathParams substitutes path parameters with example values (OpenAPI-style).
func FillPathParams(op model.Operation) string {
	p := op.Path
	for _, param := range op.Parameters {
		if param.In != "path" {
			continue
		}
		ph := "{" + param.Name + "}"
		if !strings.Contains(p, ph) {
			continue
		}
		p = strings.ReplaceAll(p, ph, examplePathValue(param))
	}
	return p
}

func examplePathValue(p model.Parameter) string {
	if p.Schema == nil {
		return "1"
	}
	switch p.Schema.Type {
	case "integer", "number":
		return "1"
	case "string":
		if len(p.Schema.Enum) > 0 {
			return fmt.Sprint(p.Schema.Enum[0])
		}
		return "x"
	default:
		return "1"
	}
}

// ExampleVariables returns a deterministic variables map for op.GQLVariableTypes.
func ExampleVariables(op model.Operation) map[string]any {
	if len(op.GQLVariableTypes) == 0 {
		return nil
	}
	out := make(map[string]any, len(op.GQLVariableTypes))
	seed := 4242
	for name, sch := range op.GQLVariableTypes {
		if sch == nil {
			out[name] = nil
			continue
		}
		out[name] = gen.GenForSchema(sch).Example(seed)
		seed++
	}
	return out
}

// MinimalInput returns a CaseInput that sends op.GQLDocument with example variables.
func MinimalInput(op model.Operation) model.CaseInput {
	return model.CaseInput{
		Method:       op.Method,
		Path:         FillPathParams(op),
		Headers:      map[string]string{"Content-Type": "application/json"},
		GQLQuery:     op.GQLDocument,
		GQLVariables: ExampleVariables(op),
	}
}
