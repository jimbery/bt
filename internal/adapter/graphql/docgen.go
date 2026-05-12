package graphql

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/jayimbery/bt/pkg/model"
)

// gqlDocBuilder collects variable declarations while building a minimal operation document.
type gqlDocBuilder struct {
	varTypes map[string]*model.SchemaRef
}

func newGQLDocBuilder() *gqlDocBuilder {
	return &gqlDocBuilder{varTypes: make(map[string]*model.SchemaRef)}
}

func (b *gqlDocBuilder) addVar(graphQLName string, arg *ast.ArgumentDefinition, schema *ast.Schema) error {
	if arg == nil {
		return nil
	}
	if _, exists := b.varTypes[graphQLName]; exists {
		return nil
	}
	sr, err := buildSchemaRefForGraphQLType(schema, arg.Type, make(map[string]struct{}))
	if err != nil {
		return fmt.Errorf("variable %q: %w", graphQLName, err)
	}
	b.varTypes[graphQLName] = sr
	return nil
}

func rootArgNamesContains(args ast.ArgumentDefinitionList, name string) bool {
	for _, a := range args {
		if a != nil && a.Name == name {
			return true
		}
	}
	return false
}

// minimalOperationDocument returns a valid minimal GraphQL document for the root field,
// including variables for the root field's arguments and for any required nested field arguments.
func minimalOperationDocument(schema *ast.Schema, gk model.GQLOperationKind, field *ast.FieldDefinition) (string, map[string]*model.SchemaRef, error) {
	b := newGQLDocBuilder()
	for _, arg := range field.Arguments {
		if arg == nil {
			continue
		}
		if err := b.addVar(arg.Name, arg, schema); err != nil {
			return "", nil, err
		}
	}

	root := gqlRootKeyword(gk)
	var rootVarDecls []string
	for _, arg := range field.Arguments {
		if arg == nil {
			continue
		}
		rootVarDecls = append(rootVarDecls, "$"+arg.Name+": "+astTypeToSDL(arg.Type))
	}
	var rootCallArgs []string
	for _, arg := range field.Arguments {
		if arg == nil {
			continue
		}
		rootCallArgs = append(rootCallArgs, arg.Name+": $"+arg.Name)
	}
	argPart := ""
	if len(rootCallArgs) > 0 {
		argPart = "(" + strings.Join(rootCallArgs, ", ") + ")"
	}

	named := unwrapNamedDef(schema, field.Type)
	if named == nil {
		return "", nil, fmt.Errorf("cannot resolve return type for field %q", field.Name)
	}
	sel, err := selectionSetStringWithSeen(schema, named, make(map[string]struct{}), b)
	if err != nil {
		return "", nil, err
	}

	var nestedDecls []string
	for vn := range b.varTypes {
		if rootArgNamesContains(field.Arguments, vn) {
			continue
		}
		// Recover SDL type from schema by locating the ArgumentDefinition that introduced vn.
		sdl, err := nestedVarSDL(schema, vn)
		if err != nil {
			return "", nil, err
		}
		nestedDecls = append(nestedDecls, "$"+vn+": "+sdl)
	}
	sort.Strings(nestedDecls)

	allDecls := append(append([]string{}, rootVarDecls...), nestedDecls...)
	varDecl := ""
	if len(allDecls) > 0 {
		varDecl = "(" + strings.Join(allDecls, ", ") + ")"
	}

	var doc string
	if sel == "" {
		doc = fmt.Sprintf("%s%s { %s%s }", root, varDecl, field.Name, argPart)
	} else {
		doc = fmt.Sprintf("%s%s { %s%s %s }", root, varDecl, field.Name, argPart, sel)
	}
	return doc, b.varTypes, nil
}

// nestedVarSDL maps names like "requiredExpenditureCalculator_input" back to GraphQL input SDL.
func nestedVarSDL(schema *ast.Schema, varName string) (string, error) {
	idx := strings.Index(varName, "_")
	if idx <= 0 || idx == len(varName)-1 {
		return "", fmt.Errorf("nested variable %q: expected field_arg shape", varName)
	}
	fieldName := varName[:idx]
	argName := varName[idx+1:]
	for _, def := range schema.Types {
		if def == nil || (def.Kind != ast.Object && def.Kind != ast.Interface) {
			continue
		}
		sdl, err := nestedVarSDLFromObject(def, fieldName, argName, varName)
		if err != nil {
			return "", err
		}
		if sdl != "" {
			return sdl, nil
		}
	}
	return "", fmt.Errorf("nested variable %q: could not resolve argument SDL", varName)
}

func nestedVarSDLFromObject(def *ast.Definition, wantField, wantArg, varName string) (string, error) {
	for _, f := range def.Fields {
		if f == nil || f.Name != wantField {
			continue
		}
		for _, a := range f.Arguments {
			if a != nil && a.Name == wantArg {
				return astTypeToSDL(a.Type), nil
			}
		}
		return "", fmt.Errorf("nested variable %q: field %q has no arg %q on type %q", varName, wantField, wantArg, def.Name)
	}
	return "", nil
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

func fieldWithArgs(schema *ast.Schema, f *ast.FieldDefinition, b *gqlDocBuilder) (string, error) {
	if f == nil || len(f.Arguments) == 0 {
		if f == nil {
			return "", nil
		}
		return f.Name, nil
	}
	var parts []string
	for _, arg := range f.Arguments {
		if arg == nil {
			continue
		}
		vn := f.Name + "_" + arg.Name
		if err := b.addVar(vn, arg, schema); err != nil {
			return "", err
		}
		parts = append(parts, arg.Name+": $"+vn)
	}
	return f.Name + "(" + strings.Join(parts, ", ") + ")", nil
}

func selectionSetStringWithSeen(schema *ast.Schema, def *ast.Definition, seen map[string]struct{}, b *gqlDocBuilder) (string, error) {
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
			call, err := fieldWithArgs(schema, f, b)
			if err != nil {
				return "", err
			}
			switch inner.Kind {
			case ast.Scalar, ast.Enum:
				parts = append(parts, call)
			case ast.Object, ast.Interface:
				sub, err := selectionSetStringWithSeen(schema, inner, seen, b)
				if err != nil {
					return "", err
				}
				if sub != "" {
					parts = append(parts, call+" "+sub)
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
