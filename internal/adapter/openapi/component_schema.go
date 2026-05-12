package openapi

import (
	"fmt"
	"strings"

	"github.com/pb33f/libopenapi"
	"github.com/pb33f/libopenapi/datamodel/high/base"

	"github.com/jayimbery/bt/pkg/model"
)

const componentsSchemasPrefix = "#/components/schemas/"

// ResolveComponentSchema resolves ref to a component schema (only "#/components/schemas/Name").
func ResolveComponentSchema(openAPIYAML []byte, ref string) (*model.SchemaRef, error) {
	ref = strings.TrimSpace(ref)
	if !strings.HasPrefix(ref, componentsSchemasPrefix) {
		return nil, fmt.Errorf("unsupported schema ref %q (want prefix %q)", ref, componentsSchemasPrefix)
	}
	name := strings.TrimPrefix(ref, componentsSchemasPrefix)
	if name == "" {
		return nil, fmt.Errorf("empty schema name in ref %q", ref)
	}

	doc, err := libopenapi.NewDocument(openAPIYAML)
	if err != nil {
		return nil, fmt.Errorf("parse OpenAPI document: %w", err)
	}
	v3model, err := doc.BuildV3Model()
	if err != nil {
		return nil, fmt.Errorf("build OpenAPI v3 model: %w", err)
	}
	comps := v3model.Model.Components
	if comps == nil || comps.Schemas == nil {
		return nil, fmt.Errorf("OpenAPI document has no components.schemas")
	}
	var proxy *base.SchemaProxy
	for pair := comps.Schemas.First(); pair != nil; pair = pair.Next() {
		if pair.Key() == name {
			proxy = pair.Value()
			break
		}
	}
	if proxy == nil {
		return nil, fmt.Errorf("no component schema %q", name)
	}
	return normaliseSchemaFromProxy(proxy, 0, map[*base.Schema]struct{}{}), nil
}
