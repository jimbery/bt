package openapi

import (
	"strings"

	"github.com/pb33f/libopenapi/datamodel/high/base"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"go.yaml.in/yaml/v4"

	"github.com/jayimbery/bt/pkg/model"
)

const maxSchemaDepth = 24

func operationsFromPathItem(item *v3.PathItem) map[string]*v3.Operation {
	if item == nil {
		return nil
	}
	m := make(map[string]*v3.Operation)
	om := item.GetOperations()
	if om == nil {
		return m
	}
	for method, op := range om.FromOldest() {
		if op != nil {
			m[method] = op
		}
	}
	return m
}

func normaliseOperation(method, path string, op *v3.Operation) model.Operation {
	if op == nil {
		return model.Operation{Method: method, Path: path}
	}
	id := op.OperationId
	if id == "" {
		id = generateID(method, path)
	}
	method = strings.ToUpper(method)
	out := model.Operation{
		ID:     id,
		Method: method,
		Path:   path,
		Tags:   append([]string(nil), op.Tags...),
	}
	seen := make(map[*base.Schema]struct{})
	for _, p := range op.Parameters {
		if p == nil {
			continue
		}
		param := model.Parameter{
			Name:     p.Name,
			In:       p.In,
			Required: p.Required != nil && *p.Required,
		}
		if p.Schema != nil {
			param.Schema = normaliseSchemaFromProxy(p.Schema, 0, seen)
		}
		out.Parameters = append(out.Parameters, param)
	}
	if op.RequestBody != nil && op.RequestBody.Content != nil {
		for _, mediaType := range op.RequestBody.Content.FromOldest() {
			if mediaType != nil && mediaType.Schema != nil {
				out.RequestBody = normaliseSchemaFromProxy(mediaType.Schema, 0, seen)
			}
			break
		}
	}
	if op.Responses != nil && op.Responses.Codes != nil {
		for code, resp := range op.Responses.Codes.FromOldest() {
			if resp == nil {
				continue
			}
			statusCode := parseStatusCode(code)
			if statusCode == 0 && code != "0" {
				continue
			}
			rs := model.ResponseSpec{StatusCode: statusCode}
			if resp.Content != nil {
				for _, mediaType := range resp.Content.FromOldest() {
					if mediaType != nil && mediaType.Schema != nil {
						rs.Schema = normaliseSchemaFromProxy(mediaType.Schema, 0, seen)
					}
					break
				}
			}
			out.Responses = append(out.Responses, rs)
		}
	}
	return out
}

func normaliseSchemaFromProxy(proxy *base.SchemaProxy, depth int, seen map[*base.Schema]struct{}) *model.SchemaRef {
	if proxy == nil {
		return nil
	}
	s := proxy.Schema()
	if s == nil {
		return &model.SchemaRef{}
	}
	return normaliseSchemaFromBase(s, depth, seen)
}

func normaliseSchemaFromBase(s *base.Schema, depth int, seen map[*base.Schema]struct{}) *model.SchemaRef {
	if s == nil {
		return nil
	}
	if depth > maxSchemaDepth {
		ref := &model.SchemaRef{}
		if len(s.Type) > 0 {
			ref.Type = s.Type[0]
		}
		return ref
	}
	if _, dup := seen[s]; dup {
		ref := &model.SchemaRef{}
		if len(s.Type) > 0 {
			ref.Type = s.Type[0]
		} else {
			ref.Type = "object"
		}
		return ref
	}
	seen[s] = struct{}{}
	defer delete(seen, s)

	ref := &model.SchemaRef{}
	if len(s.Type) > 0 {
		ref.Type = s.Type[0]
	}
	ref.Format = s.Format
	if s.Nullable != nil {
		ref.Nullable = *s.Nullable
	}
	ref.Required = append([]string(nil), s.Required...)
	for _, n := range s.Enum {
		if v := yamlNodeToAny(n); v != nil {
			ref.Enum = append(ref.Enum, v)
		}
	}
	if s.Properties != nil {
		ref.Properties = make(map[string]*model.SchemaRef)
		for name, child := range s.Properties.FromOldest() {
			if child == nil {
				continue
			}
			ref.Properties[name] = normaliseSchemaFromProxy(child, depth+1, seen)
		}
	}
	if s.Items != nil && s.Items.IsA() && s.Items.A != nil {
		ref.Items = normaliseSchemaFromProxy(s.Items.A, depth+1, seen)
	}
	for _, child := range s.OneOf {
		if child == nil {
			continue
		}
		ref.OneOf = append(ref.OneOf, normaliseSchemaFromProxy(child, depth+1, seen))
	}
	for _, child := range s.AnyOf {
		if child == nil {
			continue
		}
		ref.AnyOf = append(ref.AnyOf, normaliseSchemaFromProxy(child, depth+1, seen))
	}
	return ref
}

func yamlNodeToAny(n *yaml.Node) any {
	if n == nil {
		return nil
	}
	var v any
	if err := n.Decode(&v); err != nil {
		return n.Value
	}
	return v
}
