package graphql

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/vektah/gqlparser/v2"
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/jayimbery/bt/internal/adapter"
	"github.com/jayimbery/bt/pkg/model"
)

const defaultGraphQLPath = "/graphql"

// DefaultHTTPTimeout bounds HTTP calls for introspection.
const DefaultHTTPTimeout = 60 * time.Second

// Adapter implements adapter.Adapter for GraphQL SDL and introspection.
type Adapter struct {
	httpClient *http.Client
}

// New returns a GraphQL adapter.
func New() adapter.Adapter {
	return &Adapter{httpClient: &http.Client{Timeout: DefaultHTTPTimeout}}
}

func (a *Adapter) Name() string { return "graphql" }

func graphqlPath(t model.Target) string {
	p := strings.TrimSpace(t.GraphQLPath)
	if p == "" {
		return defaultGraphQLPath
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

// Discover returns operations from SDL (SchemaPath set) or introspection (BaseURL).
func (a *Adapter) Discover(ctx context.Context, target model.Target) ([]model.Operation, error) {
	path := graphqlPath(target)
	if strings.TrimSpace(target.SchemaPath) != "" {
		return discoverFromSDL(target.SchemaPath, path)
	}
	if strings.TrimSpace(target.BaseURL) != "" {
		return a.discoverFromIntrospection(ctx, target.BaseURL, path)
	}
	return nil, fmt.Errorf("graphql adapter: need target.schema (SDL path) or target.base_url for introspection")
}

// Validate checks SDL file when SchemaPath is set; otherwise probes introspection.
func (a *Adapter) Validate(ctx context.Context, target model.Target) error {
	if strings.TrimSpace(target.SchemaPath) != "" {
		if _, err := os.Stat(target.SchemaPath); err != nil {
			return fmt.Errorf("graphql validate: schema file: %w", err)
		}
		_, err := loadSchemaFromFile(target.SchemaPath)
		return err
	}
	base := strings.TrimSpace(target.BaseURL)
	if base == "" {
		return fmt.Errorf("graphql validate: target.schema or target.base_url is required")
	}
	return a.validateIntrospectionReachable(ctx, base, graphqlPath(target))
}

func loadSchemaFromFile(schemaPath string) (*ast.Schema, error) {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, fmt.Errorf("read SDL: %w", err)
	}
	schema, err := gqlparser.LoadSchema(&ast.Source{Name: schemaPath, Input: string(data)})
	if err != nil {
		return nil, fmt.Errorf("parse SDL: %w", err)
	}
	return schema, nil
}

func discoverFromSDL(schemaPath, httpPath string) ([]model.Operation, error) {
	schema, err := loadSchemaFromFile(schemaPath)
	if err != nil {
		return nil, err
	}
	var out []model.Operation
	for _, kind := range []struct {
		def *ast.Definition
		gk  model.GQLOperationKind
	}{
		{schema.Query, model.GQLQuery},
		{schema.Mutation, model.GQLMutation},
		{schema.Subscription, model.GQLSubscription},
	} {
		if kind.def == nil {
			continue
		}
		for _, field := range kind.def.Fields {
			if field == nil || isDeprecated(field.Directives) {
				continue
			}
			if strings.HasPrefix(field.Name, "__") {
				continue
			}
			op, err := operationFromField(schema, field, kind.gk, httpPath)
			if err != nil {
				return nil, fmt.Errorf("operation %q: %w", field.Name, err)
			}
			out = append(out, op)
		}
	}
	return out, nil
}

func operationFromField(schema *ast.Schema, field *ast.FieldDefinition, gk model.GQLOperationKind, httpPath string) (model.Operation, error) {
	selSchema, err := selectionSchemaForType(schema, field.Type)
	if err != nil {
		return model.Operation{}, err
	}
	doc, err := minimalOperationDocument(schema, gk, field)
	if err != nil {
		return model.Operation{}, err
	}
	varTypes, err := variableTypesFromArgs(schema, field.Arguments)
	if err != nil {
		return model.Operation{}, err
	}
	env, err := responseEnvelopeSchema(field.Name, selSchema)
	if err != nil {
		return model.Operation{}, err
	}
	return model.Operation{
		ID:                 field.Name,
		Method:             http.MethodPost,
		Path:               httpPath,
		GQLKind:            gk,
		GQLDocument:        doc,
		GQLVariableTypes:   varTypes,
		GQLSelectionSchema: selSchema,
		Responses:          []model.ResponseSpec{{StatusCode: 200, Schema: env}},
	}, nil
}

func isDeprecated(dirs ast.DirectiveList) bool {
	for _, d := range dirs {
		if d != nil && d.Name == "deprecated" {
			return true
		}
	}
	return false
}

func (a *Adapter) validateIntrospectionReachable(ctx context.Context, baseURL, gqlPath string) error {
	u := strings.TrimRight(baseURL, "/") + gqlPath
	body := []byte(`{"query":"{ __schema { queryType { name } } }"}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("graphql introspection validate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("graphql introspection validate: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if !jsonHasSchemaKey(b) {
		return fmt.Errorf("graphql introspection validate: response missing __schema")
	}
	return nil
}

func (a *Adapter) discoverFromIntrospection(ctx context.Context, baseURL, gqlPath string) ([]model.Operation, error) {
	u := strings.TrimRight(baseURL, "/") + gqlPath
	payload, err := json.Marshal(map[string]string{"query": introspectionQuery})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graphql introspection: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("graphql introspection: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return parseIntrospectionOperations(b, gqlPath)
}
