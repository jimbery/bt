package graphql

import (
	"fmt"

	"github.com/vektah/gqlparser/v2/ast"

	"github.com/jimbery/bt/pkg/model"
)

func unwrapNamedDef(s *ast.Schema, t *ast.Type) *ast.Definition {
	if t == nil {
		return nil
	}
	if t.NamedType != "" {
		return s.Types[t.NamedType]
	}
	if t.Elem != nil {
		return unwrapNamedDef(s, t.Elem)
	}
	return nil
}

// isNonNull reports whether a GraphQL field's outer type is non-null (field is required).
func isNonNull(t *ast.Type) bool {
	return t != nil && t.NonNull
}

func selectionSchemaForType(s *ast.Schema, fieldType *ast.Type) (*model.SchemaRef, error) {
	seen := make(map[string]struct{})
	return buildSchemaRefForGraphQLType(s, fieldType, seen)
}

func buildSchemaRefForGraphQLType(s *ast.Schema, t *ast.Type, seen map[string]struct{}) (*model.SchemaRef, error) {
	if t == nil {
		return nil, fmt.Errorf("nil GraphQL type")
	}
	if t.Elem != nil {
		inner, err := buildSchemaRefForGraphQLType(s, t.Elem, seen)
		if err != nil {
			return nil, err
		}
		return &model.SchemaRef{
			Type:     "array",
			Items:    inner,
			Nullable: !t.NonNull,
		}, nil
	}
	if t.NamedType != "" {
		def := s.Types[t.NamedType]
		if def == nil {
			return nil, fmt.Errorf("unknown type %q", t.NamedType)
		}
		return definitionToSchemaRef(s, def, !t.NonNull, seen)
	}
	return nil, fmt.Errorf("unexpected GraphQL type")
}

func definitionToSchemaRef(s *ast.Schema, def *ast.Definition, nullable bool, seen map[string]struct{}) (*model.SchemaRef, error) {
	switch def.Kind {
	case ast.Scalar:
		sch := scalarToSchemaRef(def.Name)
		sch.Nullable = nullable
		return sch, nil
	case ast.Enum:
		vals := make([]any, 0, len(def.EnumValues))
		for _, ev := range def.EnumValues {
			if ev == nil || isDeprecated(ev.Directives) {
				continue
			}
			vals = append(vals, ev.Name)
		}
		return &model.SchemaRef{Type: "string", Enum: vals, Nullable: nullable}, nil
	case ast.Object, ast.Interface:
		if def.Name != "" {
			if _, dup := seen[def.Name]; dup {
				return &model.SchemaRef{Type: "object", Nullable: nullable}, nil
			}
			seen[def.Name] = struct{}{}
			defer delete(seen, def.Name)
		}
		props := make(map[string]*model.SchemaRef)
		var required []string
		for _, f := range def.Fields {
			if f == nil || isDeprecated(f.Directives) {
				continue
			}
			sub, err := buildSchemaRefForGraphQLType(s, f.Type, seen)
			if err != nil {
				return nil, err
			}
			props[f.Name] = sub
			if isNonNull(f.Type) {
				required = append(required, f.Name)
			}
		}
		return &model.SchemaRef{
			Type:       "object",
			Properties: props,
			Required:   required,
			Nullable:   nullable,
		}, nil
	case ast.InputObject:
		if def.Name != "" {
			if _, dup := seen[def.Name]; dup {
				return &model.SchemaRef{Type: "object", Nullable: nullable}, nil
			}
			seen[def.Name] = struct{}{}
			defer delete(seen, def.Name)
		}
		props := make(map[string]*model.SchemaRef)
		var required []string
		for _, f := range def.Fields {
			if f == nil || isDeprecated(f.Directives) {
				continue
			}
			sub, err := buildSchemaRefForGraphQLType(s, f.Type, seen)
			if err != nil {
				return nil, err
			}
			props[f.Name] = sub
			if isNonNull(f.Type) {
				required = append(required, f.Name)
			}
		}
		return &model.SchemaRef{
			Type:       "object",
			Properties: props,
			Required:   required,
			Nullable:   nullable,
		}, nil
	case ast.Union:
		return &model.SchemaRef{Type: "object", Nullable: nullable}, nil
	default:
		return &model.SchemaRef{Type: "object", Nullable: nullable}, nil
	}
}

func scalarToSchemaRef(name string) *model.SchemaRef {
	switch name {
	case "String":
		return &model.SchemaRef{Type: "string"}
	case "ID":
		return &model.SchemaRef{Type: "string", Format: "id"}
	case "Int":
		return &model.SchemaRef{Type: "integer"}
	case "Float":
		return &model.SchemaRef{Type: "number"}
	case "Boolean":
		return &model.SchemaRef{Type: "boolean"}
	case "JSON", "Json":
		// Arbitrary JSON: omit Type so response validation treats values as unconstrained (see validate.validateValue default).
		return &model.SchemaRef{}
	default:
		return &model.SchemaRef{Type: "string"}
	}
}

func responseEnvelopeSchema(fieldName string, sel *model.SchemaRef) (*model.SchemaRef, error) {
	if fieldName == "" {
		return nil, fmt.Errorf("empty field name")
	}
	dataShape := &model.SchemaRef{
		Type:     "object",
		Nullable: true,
		Properties: map[string]*model.SchemaRef{
			fieldName: sel,
		},
	}
	errItem := &model.SchemaRef{
		Type: "object",
		Properties: map[string]*model.SchemaRef{
			"message": {Type: "string"},
		},
		Required: []string{"message"},
	}
	return &model.SchemaRef{
		Type: "object",
		Properties: map[string]*model.SchemaRef{
			"data": dataShape,
			"errors": {
				Type:     "array",
				Nullable: true,
				Items:    errItem,
			},
		},
	}, nil
}

func astTypeToSDL(t *ast.Type) string {
	if t == nil {
		return ""
	}
	return t.String()
}
