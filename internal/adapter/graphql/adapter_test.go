package graphql_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	gqladapter "github.com/jayimbery/bt/internal/adapter/graphql"
	"github.com/jayimbery/bt/pkg/model"
)

func writeSDL(t *testing.T, sdl string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.graphql")
	if err := os.WriteFile(path, []byte(sdl), 0o644); err != nil {
		t.Fatalf("writeSDL: %v", err)
	}
	return path
}

const minimalSDL = `
type Query {
  product(id: ID!): Product
  products(filter: String): [Product!]!
}

type Mutation {
  createProduct(name: String!, price: Float!): Product!
  deleteProduct(id: ID!): Boolean!
}

type Subscription {
  productUpdated(id: ID!): Product
}

type Product {
  id: ID!
  name: String!
  price: Float!
  description: String
  inStock: Boolean!
}
`

func TestGraphQLAdapter_Name_IsGraphQL(t *testing.T) {
	t.Parallel()
	a := gqladapter.New()
	if a.Name() != "graphql" {
		t.Errorf("expected adapter name 'graphql', got %q", a.Name())
	}
}

func TestGraphQLAdapter_Discover_SDL_ReturnsAllOperations(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 5 {
		t.Errorf("expected 5 operations, got %d", len(ops))
	}
}

func TestGraphQLAdapter_Discover_SDL_QueryFieldsHaveGQLKindQuery(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	queries := 0
	for _, op := range ops {
		if op.GQLKind == model.GQLQuery {
			queries++
		}
	}
	if queries != 2 {
		t.Errorf("expected 2 Query operations, got %d", queries)
	}
}

func TestGraphQLAdapter_Discover_SDL_MutationFieldsHaveGQLKindMutation(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mutations := 0
	for _, op := range ops {
		if op.GQLKind == model.GQLMutation {
			mutations++
		}
	}
	if mutations != 2 {
		t.Errorf("expected 2 Mutation operations, got %d", mutations)
	}
}

func TestGraphQLAdapter_Discover_SDL_SubscriptionFieldsHaveGQLKindSubscription(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	subs := 0
	for _, op := range ops {
		if op.GQLKind == model.GQLSubscription {
			subs++
		}
	}
	if subs != 1 {
		t.Errorf("expected 1 Subscription operation, got %d", subs)
	}
}

func TestGraphQLAdapter_Discover_SDL_OperationID_IsFieldName(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ids := make(map[string]bool)
	for _, op := range ops {
		ids[op.ID] = true
	}
	for _, want := range []string{"product", "products", "createProduct", "deleteProduct", "productUpdated"} {
		if !ids[want] {
			t.Errorf("expected operation with ID %q, not found", want)
		}
	}
}

func TestGraphQLAdapter_Discover_SDL_MethodIsAlwaysPOST(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.Method != "POST" {
			t.Errorf("operation %q: Method must be POST, got %q", op.ID, op.Method)
		}
	}
}

func TestGraphQLAdapter_Discover_SDL_PathIsGraphQLEndpoint(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.Path != "/graphql" {
			t.Errorf("operation %q: Path must be /graphql, got %q", op.ID, op.Path)
		}
	}
}

func TestGraphQLAdapter_Discover_SDL_GQLDocumentIsNonEmpty(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.GQLDocument == "" {
			t.Errorf("operation %q: GQLDocument must be non-empty", op.ID)
		}
	}
}

func TestGraphQLAdapter_Discover_SDL_RequiredArgument_VariableTypeIsNonNullable(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "product" {
			idType, ok := op.GQLVariableTypes["id"]
			if !ok {
				t.Fatal("expected variable type for 'id' on 'product' operation")
			}
			if idType.Type != "string" {
				t.Errorf("ID type must map to string, got %q", idType.Type)
			}
			if idType.Nullable {
				t.Error("ID! (non-null) must have Nullable=false")
			}
			return
		}
	}
	t.Error("operation 'product' not found")
}

func TestGraphQLAdapter_Discover_SDL_OptionalArgument_VariableTypeIsNullable(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "products" {
			filterType, ok := op.GQLVariableTypes["filter"]
			if !ok {
				t.Fatal("expected variable type for 'filter' on 'products' operation")
			}
			if !filterType.Nullable {
				t.Error("nullable argument must have Nullable=true")
			}
			return
		}
	}
	t.Error("operation 'products' not found")
}

func TestGraphQLAdapter_Discover_SDL_FloatArgument_MapsToNumber(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "createProduct" {
			priceType, ok := op.GQLVariableTypes["price"]
			if !ok {
				t.Fatal("expected variable type for 'price' on 'createProduct'")
			}
			if priceType.Type != "number" {
				t.Errorf("Float must map to 'number', got %q", priceType.Type)
			}
			return
		}
	}
	t.Error("operation 'createProduct' not found")
}

func TestGraphQLAdapter_Discover_SDL_SelectionSchema_HasExpectedFields(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "product" {
			if op.GQLSelectionSchema == nil {
				t.Fatal("GQLSelectionSchema must be non-nil for 'product'")
			}
			if op.GQLSelectionSchema.Type != "object" {
				t.Errorf("selection schema type: got %q, want object", op.GQLSelectionSchema.Type)
			}
			for _, field := range []string{"id", "name", "price", "inStock"} {
				if _, ok := op.GQLSelectionSchema.Properties[field]; !ok {
					t.Errorf("expected field %q in selection schema properties", field)
				}
			}
			return
		}
	}
	t.Error("operation 'product' not found")
}

func TestGraphQLAdapter_Discover_SDL_NonNullField_MarkedNotNullable(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "product" {
			nameSchema, ok := op.GQLSelectionSchema.Properties["name"]
			if !ok {
				t.Fatal("expected 'name' in selection schema")
			}
			if nameSchema.Nullable {
				t.Error("String! field must have Nullable=false")
			}
			return
		}
	}
	t.Error("operation 'product' not found")
}

func TestGraphQLAdapter_Discover_SDL_NullableField_MarkedNullable(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "product" {
			descSchema, ok := op.GQLSelectionSchema.Properties["description"]
			if !ok {
				t.Fatal("expected 'description' in selection schema")
			}
			if !descSchema.Nullable {
				t.Error("String (nullable) field must have Nullable=true")
			}
			return
		}
	}
	t.Error("operation 'product' not found")
}

func TestGraphQLAdapter_Discover_SDL_ResponseSchema_HasDataAndErrorsKeys(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "product" {
			if len(op.Responses) == 0 {
				t.Fatal("expected at least one ResponseSpec")
			}
			resp200 := op.Responses[0]
			if resp200.StatusCode != 200 {
				t.Errorf("expected 200 status code, got %d", resp200.StatusCode)
			}
			if resp200.Schema == nil {
				t.Fatal("response schema must be non-nil")
			}
			if _, ok := resp200.Schema.Properties["data"]; !ok {
				t.Error("response envelope schema must have 'data' property")
			}
			if _, ok := resp200.Schema.Properties["errors"]; !ok {
				t.Error("response envelope schema must have 'errors' property")
			}
			return
		}
	}
	t.Error("operation 'product' not found")
}

func TestGraphQLAdapter_Validate_ValidSDL_ReturnsNil(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	if err := a.Validate(context.Background(), model.Target{SchemaPath: path}); err != nil {
		t.Errorf("expected Validate to pass for valid SDL, got: %v", err)
	}
}

func TestGraphQLAdapter_Validate_MissingFile_ReturnsError(t *testing.T) {
	t.Parallel()
	a := gqladapter.New()
	err := a.Validate(context.Background(), model.Target{SchemaPath: "/no/such/schema.graphql"})
	if err == nil {
		t.Error("expected error for missing SDL file, got nil")
	}
}

func TestGraphQLAdapter_Validate_MalformedSDL_ReturnsError(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, `type Query { this is not valid SDL }`)
	a := gqladapter.New()
	err := a.Validate(context.Background(), model.Target{SchemaPath: path})
	if err == nil {
		t.Error("expected error for malformed SDL, got nil")
	}
}

func TestGraphQLAdapter_Discover_Introspection_UsedWhenNoSDLPath(t *testing.T) {
	t.Parallel()
	introspectionResponse := map[string]any{
		"data": map[string]any{
			"__schema": map[string]any{
				"queryType":    map[string]any{"name": "Query"},
				"mutationType": nil,
				"types": []any{
					map[string]any{
						"kind": "OBJECT",
						"name": "Query",
						"fields": []any{
							map[string]any{
								"name":         "ping",
								"args":         []any{},
								"isDeprecated": false,
								"type": map[string]any{
									"kind":   "SCALAR",
									"name":   "String",
									"ofType": nil,
								},
							},
						},
					},
				},
			},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(introspectionResponse)
	}))
	defer srv.Close()
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected error from introspection discovery: %v", err)
	}
	if len(ops) == 0 {
		t.Error("expected at least one operation from introspection, got none")
	}
	if ops[0].ID != "ping" {
		t.Errorf("expected operation 'ping', got %q", ops[0].ID)
	}
}

func TestGraphQLAdapter_Discover_Introspection_ServerError_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	a := gqladapter.New()
	_, err := a.Discover(context.Background(), model.Target{BaseURL: srv.URL})
	if err == nil {
		t.Error("expected error when introspection is forbidden, got nil")
	}
}

func TestGraphQLAdapter_Discover_SDL_EnumType_MapsToStringWithEnumValues(t *testing.T) {
	t.Parallel()
	sdl := `
type Query {
  ordersByStatus(status: OrderStatus!): [String!]!
}

enum OrderStatus {
  PENDING
  CONFIRMED
  CANCELLED
}
`
	path := writeSDL(t, sdl)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "ordersByStatus" {
			statusType, ok := op.GQLVariableTypes["status"]
			if !ok {
				t.Fatal("expected variable type for 'status'")
			}
			if statusType.Type != "string" {
				t.Errorf("enum must map to string, got %q", statusType.Type)
			}
			if len(statusType.Enum) != 3 {
				t.Errorf("expected 3 enum values, got %d: %v", len(statusType.Enum), statusType.Enum)
			}
			return
		}
	}
	t.Error("operation 'ordersByStatus' not found")
}

func TestGraphQLAdapter_Discover_SDL_ListReturnType_MapsToArray(t *testing.T) {
	t.Parallel()
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()
	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, op := range ops {
		if op.ID == "products" {
			if op.GQLSelectionSchema == nil {
				t.Fatal("GQLSelectionSchema must be non-nil for list operation")
			}
			if op.GQLSelectionSchema.Type != "array" {
				t.Errorf("list return type must map to array, got %q", op.GQLSelectionSchema.Type)
			}
			if op.GQLSelectionSchema.Items == nil {
				t.Error("array schema must have Items set for list return type")
			}
			return
		}
	}
	t.Error("operation 'products' not found")
}
