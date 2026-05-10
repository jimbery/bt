package graphql

import (
	"fmt"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/jayimbery/bt/pkg/model"
)

func minimalOperationDocument(schema *ast.Schema, gk model.GQLOperationKind, field *ast.FieldDefinition) (string, error) {
	root := gqlRootKeyword(gk)
	var varDecls []string
	for _, arg := range field.Arguments {
		if arg == nil {
			continue
		}
		varDecls = append(varDecls, "$"+arg.Name+": "+astTypeToSDL(arg.Type))
	}
	varDecl := ""
	if len(varDecls) > 0 {
		varDecl = "(" + strings.Join(varDecls, ", ") + ")"
	}
	var callArgs []string
	for _, arg := range field.Arguments {
		if arg == nil {
			continue
		}
		callArgs = append(callArgs, arg.Name+": $"+arg.Name)
	}
	argPart := ""
	if len(callArgs) > 0 {
		argPart = "(" + strings.Join(callArgs, ", ") + ")"
	}
	named := unwrapNamedDef(schema, field.Type)
	if named == nil {
		return "", fmt.Errorf("cannot resolve return type for field %q", field.Name)
	}
	sel, err := selectionSetStringWithSeen(schema, named, make(map[string]struct{}))
	if err != nil {
		return "", err
	}
	if sel == "" {
		return fmt.Sprintf("%s%s { %s%s }", root, varDecl, field.Name, argPart), nil
	}
	return fmt.Sprintf("%s%s { %s%s %s }", root, varDecl, field.Name, argPart, sel), nil
}

func gqlRootKeyword(k model.GQLOperationKind) string {
	switch k {
	case model.GQLMutation:
		return "mutation"
	case model.GQLSubscription:
		return "subscription"
	default:
		return "query"
	}
}

func selectionSetString(schema *ast.Schema, def *ast.Definition) (string, error) {
	return selectionSetStringWithSeen(schema, def, make(map[string]struct{}))
}

func selectionSetStringWithSeen(schema *ast.Schema, def *ast.Definition, seen map[string]struct{}) (string, error) {
	switch def.Kind {
	case ast.Scalar, ast.Enum:
		return "", nil
	case ast.Object, ast.Interface:
		if def.Name != "" {
			if _, dup := seen[def.Name]; dup {
				return "{ __typename }", nil
			}
			seen[def.Name] = struct{}{}
			defer delete(seen, def.Name)
		}
		var parts []string
		for _, f := range def.Fields {
			if f == nil || isDeprecated(f.Directives) {
				continue
			}
			inner := unwrapNamedDef(schema, f.Type)
			if inner == nil {
				continue
			}
			switch inner.Kind {
			case ast.Scalar, ast.Enum:
				parts = append(parts, f.Name)
			case ast.Object, ast.Interface:
				sub, err := selectionSetStringWithSeen(schema, inner, seen)
				if err != nil {
					return "", err
				}
				if sub != "" {
					parts = append(parts, f.Name+" "+sub)
				}
			case ast.Union:
				// minimal: skip union fields without inline fragments
			default:
			}
		}
		if len(parts) == 0 {
			return "{ __typename }", nil
		}
		return "{ " + strings.Join(parts, " ") + " }", nil
	default:
		return "{ __typename }", nil
	}
}
