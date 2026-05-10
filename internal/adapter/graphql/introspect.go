package graphql

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/jayimbery/bt/pkg/model"
)

// introspectionQuery is the standard introspection document (trimmed ofType depth).
const introspectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind
      name
      fields(includeDeprecated: true) {
        name
        isDeprecated
        args { name type { ...FullType } }
        type { ...FullType }
      }
      enumValues(includeDeprecated: true) { name isDeprecated }
    }
  }
}
fragment FullType on __Type {
  kind
  name
  ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } }
}`

func jsonHasSchemaKey(body []byte) bool {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return false
	}
	data, _ := root["data"].(map[string]any)
	if data == nil {
		return false
	}
	sch, _ := data["__schema"].(map[string]any)
	return sch != nil
}

func parseIntrospectionOperations(body []byte, httpPath string) ([]model.Operation, error) {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, fmt.Errorf("introspection: invalid JSON: %w", err)
	}
	data, _ := root["data"].(map[string]any)
	if data == nil {
		return nil, fmt.Errorf("introspection: missing data")
	}
	sch, _ := data["__schema"].(map[string]any)
	if sch == nil {
		return nil, fmt.Errorf("introspection: missing __schema")
	}
	typesArr, _ := sch["types"].([]any)
	typesByName := make(map[string]map[string]any)
	for _, ti := range typesArr {
		tm, _ := ti.(map[string]any)
		if tm == nil {
			continue
		}
		n, _ := tm["name"].(string)
		if n != "" {
			typesByName[n] = tm
		}
	}

	var out []model.Operation
	if qt := sch["queryType"]; qt != nil {
		if qtm, ok := qt.(map[string]any); ok {
			ops, err := introRootFields(typesByName, qtm, model.GQLQuery, httpPath)
			if err != nil {
				return nil, err
			}
			out = append(out, ops...)
		}
	}
	if mt := sch["mutationType"]; mt != nil {
		if mtm, ok := mt.(map[string]any); ok {
			ops, err := introRootFields(typesByName, mtm, model.GQLMutation, httpPath)
			if err != nil {
				return nil, err
			}
			out = append(out, ops...)
		}
	}
	if st := sch["subscriptionType"]; st != nil {
		if stm, ok := st.(map[string]any); ok {
			ops, err := introRootFields(typesByName, stm, model.GQLSubscription, httpPath)
			if err != nil {
				return nil, err
			}
			out = append(out, ops...)
		}
	}
	return out, nil
}

func introRootFields(typesByName map[string]map[string]any, typeRef map[string]any, gk model.GQLOperationKind, httpPath string) ([]model.Operation, error) {
	name, _ := typeRef["name"].(string)
	tdef := typesByName[name]
	if tdef == nil {
		return nil, nil
	}
	fields, _ := tdef["fields"].([]any)
	var out []model.Operation
	for _, fi := range fields {
		fm, _ := fi.(map[string]any)
		if fm == nil {
			continue
		}
		if dep, _ := fm["isDeprecated"].(bool); dep {
			continue
		}
		fname, _ := fm["name"].(string)
		if fname == "" {
			continue
		}
		if strings.HasPrefix(fname, "__") {
			continue
		}
		ft, _ := fm["type"].(map[string]any)
		sel, err := introTypeToSelectionSchema(typesByName, ft)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fname, err)
		}
		args, _ := fm["args"].([]any)
		varTypes, err := introArgsToVarTypes(typesByName, args)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fname, err)
		}
		doc, err := introMinimalDoc(gk, fname, args, ft, typesByName)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", fname, err)
		}
		env, err := responseEnvelopeSchema(fname, sel)
		if err != nil {
			return nil, err
		}
		out = append(out, model.Operation{
			ID:                 fname,
			Method:             http.MethodPost,
			Path:               httpPath,
			GQLKind:            gk,
			GQLDocument:        doc,
			GQLVariableTypes:   varTypes,
			GQLSelectionSchema: sel,
			Responses:          []model.ResponseSpec{{StatusCode: 200, Schema: env}},
		})
	}
	return out, nil
}

func introArgsToVarTypes(typesByName map[string]map[string]any, args []any) (map[string]*model.SchemaRef, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := make(map[string]*model.SchemaRef)
	for _, ai := range args {
		am, _ := ai.(map[string]any)
		if am == nil {
			continue
		}
		aname, _ := am["name"].(string)
		tm, _ := am["type"].(map[string]any)
		sr, err := introWireTypeToSchemaRef(typesByName, tm)
		if err != nil {
			return nil, err
		}
		if aname != "" {
			out[aname] = sr
		}
	}
	return out, nil
}

func introMinimalDoc(gk model.GQLOperationKind, fieldName string, args []any, returnType map[string]any, typesByName map[string]map[string]any) (string, error) {
	root := gqlRootKeyword(gk)
	var varDecls []string
	var callArgs []string
	for _, ai := range args {
		am, _ := ai.(map[string]any)
		if am == nil {
			continue
		}
		aname, _ := am["name"].(string)
		tm, _ := am["type"].(map[string]any)
		sdl, err := introWireTypeToSDL(tm)
		if err != nil {
			return "", err
		}
		varDecls = append(varDecls, "$"+aname+": "+sdl)
		callArgs = append(callArgs, aname+": $"+aname)
	}
	varDecl := ""
	if len(varDecls) > 0 {
		varDecl = "(" + strings.Join(varDecls, ", ") + ")"
	}
	argPart := ""
	if len(callArgs) > 0 {
		argPart = "(" + strings.Join(callArgs, ", ") + ")"
	}
	named := introInnerNamedType(returnType)
	tdef := typesByName[named]
	sel := ""
	if tdef != nil {
		kind, _ := tdef["kind"].(string)
		switch kind {
		case "OBJECT", "INTERFACE":
			sel = introBuildSelection(typesByName, tdef)
		}
	}
	if sel == "" {
		return fmt.Sprintf("%s%s { %s%s }", root, varDecl, fieldName, argPart), nil
	}
	return fmt.Sprintf("%s%s { %s%s %s }", root, varDecl, fieldName, argPart, sel), nil
}

func introBuildSelection(typesByName map[string]map[string]any, obj map[string]any) string {
	fields, _ := obj["fields"].([]any)
	var parts []string
	for _, fi := range fields {
		fm, _ := fi.(map[string]any)
		if fm == nil {
			continue
		}
		if dep, _ := fm["isDeprecated"].(bool); dep {
			continue
		}
		fn, _ := fm["name"].(string)
		if fn == "" {
			continue
		}
		ft, _ := fm["type"].(map[string]any)
		inner := introInnerNamedType(ft)
		idef := typesByName[inner]
		if idef == nil {
			continue
		}
		ik, _ := idef["kind"].(string)
		switch ik {
		case "SCALAR", "ENUM":
			parts = append(parts, fn)
		case "OBJECT", "INTERFACE":
			sub := introBuildSelection(typesByName, idef)
			if sub != "" {
				parts = append(parts, fn+" "+sub)
			}
		}
	}
	if len(parts) == 0 {
		return "{ __typename }"
	}
	return "{ " + strings.Join(parts, " ") + " }"
}

func introWireTypeToSDL(t map[string]any) (string, error) {
	if t == nil {
		return "", fmt.Errorf("nil type")
	}
	kind, _ := t["kind"].(string)
	switch kind {
	case "NON_NULL":
		ot, _ := t["ofType"].(map[string]any)
		inner, err := introWireTypeToSDL(ot)
		if err != nil {
			return "", err
		}
		return inner + "!", nil
	case "LIST":
		ot, _ := t["ofType"].(map[string]any)
		inner, err := introWireTypeToSDL(ot)
		if err != nil {
			return "", err
		}
		return "[" + inner + "]", nil
	case "SCALAR", "OBJECT", "INTERFACE", "ENUM", "INPUT_OBJECT", "UNION":
		n, _ := t["name"].(string)
		return n, nil
	default:
		ot, _ := t["ofType"].(map[string]any)
		if ot != nil {
			return introWireTypeToSDL(ot)
		}
		return "", fmt.Errorf("unknown introspection kind %q", kind)
	}
}

func introInnerNamedType(t map[string]any) string {
	for t != nil {
		kind, _ := t["kind"].(string)
		if kind == "NON_NULL" || kind == "LIST" {
			t, _ = t["ofType"].(map[string]any)
			continue
		}
		n, _ := t["name"].(string)
		return n
	}
	return ""
}

func introTypeToSelectionSchema(typesByName map[string]map[string]any, t map[string]any) (*model.SchemaRef, error) {
	return introWireTypeToSchemaRef(typesByName, t)
}

func introWireTypeToSchemaRef(typesByName map[string]map[string]any, t map[string]any) (*model.SchemaRef, error) {
	if t == nil {
		return nil, fmt.Errorf("nil wire type")
	}
	kind, _ := t["kind"].(string)
	switch kind {
	case "NON_NULL":
		ot, _ := t["ofType"].(map[string]any)
		inner, err := introWireTypeToSchemaRef(typesByName, ot)
		if err != nil {
			return nil, err
		}
		inner.Nullable = false
		return inner, nil
	case "LIST":
		ot, _ := t["ofType"].(map[string]any)
		inner, err := introWireTypeToSchemaRef(typesByName, ot)
		if err != nil {
			return nil, err
		}
		return &model.SchemaRef{Type: "array", Items: inner, Nullable: true}, nil
	case "SCALAR":
		n, _ := t["name"].(string)
		sr := scalarToSchemaRef(n)
		sr.Nullable = true
		return sr, nil
	case "ENUM":
		n, _ := t["name"].(string)
		def := typesByName[n]
		vals := introEnumValues(def)
		return &model.SchemaRef{Type: "string", Enum: vals, Nullable: true}, nil
	case "OBJECT", "INTERFACE":
		n, _ := t["name"].(string)
		def := typesByName[n]
		if def == nil {
			return &model.SchemaRef{Type: "object", Nullable: true}, nil
		}
		return introObjectToSchemaRef(typesByName, def, true)
	case "UNION":
		return &model.SchemaRef{Type: "object", Nullable: true}, nil
	default:
		ot, _ := t["ofType"].(map[string]any)
		if ot != nil {
			return introWireTypeToSchemaRef(typesByName, ot)
		}
		return nil, fmt.Errorf("unsupported introspection kind %q", kind)
	}
}

func introEnumValues(def map[string]any) []any {
	if def == nil {
		return nil
	}
	evs, _ := def["enumValues"].([]any)
	var out []any
	for _, ei := range evs {
		em, _ := ei.(map[string]any)
		if em == nil {
			continue
		}
		if dep, _ := em["isDeprecated"].(bool); dep {
			continue
		}
		n, _ := em["name"].(string)
		if n != "" {
			out = append(out, n)
		}
	}
	return out
}

func introObjectToSchemaRef(typesByName map[string]map[string]any, def map[string]any, nullable bool) (*model.SchemaRef, error) {
	fields, _ := def["fields"].([]any)
	props := make(map[string]*model.SchemaRef)
	var required []string
	for _, fi := range fields {
		fm, _ := fi.(map[string]any)
		if fm == nil {
			continue
		}
		if dep, _ := fm["isDeprecated"].(bool); dep {
			continue
		}
		fn, _ := fm["name"].(string)
		ft, _ := fm["type"].(map[string]any)
		sub, err := introWireTypeToSchemaRef(typesByName, ft)
		if err != nil {
			return nil, err
		}
		props[fn] = sub
		k, _ := ft["kind"].(string)
		if k == "NON_NULL" {
			required = append(required, fn)
		}
	}
	return &model.SchemaRef{
		Type:       "object",
		Properties: props,
		Required:   required,
		Nullable:   nullable,
	}, nil
}
