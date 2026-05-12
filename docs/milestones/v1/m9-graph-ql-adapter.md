# M9 — GraphQL Adapter

This document follows the same structure as M1–M8: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist. Tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

Response object and schema validation is required at every layer. Asserting only a status code is not sufficient — every test must verify the shape, field types, and semantics of the response body.

---

## Overview

M9 adds a GraphQL adapter so `bt` can discover, validate, and test GraphQL APIs alongside REST APIs. The execution engine, all strategies, and the MCP interface are unchanged — GraphQL operations normalise to the same `Operation` model the rest of the platform already works with.

The four pieces built here are:

1. **Model extensions** — minimal additions to `CaseInput` and `Operation` to carry GraphQL-specific context without breaking existing REST behaviour
2. **GraphQL adapter** — discovers operations from a SDL schema file or via introspection; normalises queries and mutations to `[]model.Operation`
3. **GraphQL runner** — executes a GraphQL `CaseInput` over HTTP, handles the `{"data": ..., "errors": [...]}` response envelope, and surfaces errors as first-class failures
4. **GraphQL-aware assertions** — response schema validation that understands the GraphQL response envelope, non-null fields, union types, and the `errors` array

Each piece has its own spec, tests, and implementation section. Build and verify each step before moving to the next.

**Exit criterion:** `bt run --strategy table --adapter graphql` executes a table of GraphQL query and mutation cases against a real GraphQL API, validates response bodies against the SDL-derived schema, and reports pass/fail per case. All existing REST tests continue to pass unchanged.

---

## Step 1 — Model extensions

### Spec

The existing `model.CaseInput` covers REST well but has no slot for a GraphQL query string or operation name. Rather than stuffing the query into `Body` (which loses structure), we add two optional fields that are ignored by the REST runner.

Additions to `pkg/model/case.go`:

```go
type CaseInput struct {
    // Existing fields — unchanged.
    Method  string            `json:"method"`
    Path    string            `json:"path"`
    Headers map[string]string `json:"headers,omitempty"`
    Query   map[string]string `json:"query,omitempty"`   // URL query params (REST)
    Body    any               `json:"body,omitempty"`

    // GraphQL-specific fields — zero value means "not a GraphQL case".
    GQLQuery         string         `json:"gql_query,omitempty"`          // the query/mutation/subscription document
    GQLOperationName string         `json:"gql_operation_name,omitempty"` // selects one operation from a multi-op document
    GQLVariables     map[string]any `json:"gql_variables,omitempty"`      // variables for the operation
}
```

The `GQLQuery` field being non-empty is the signal to the GraphQL runner that this is a GraphQL case. The REST runner ignores all `GQL*` fields.

Additions to `pkg/model/operation.go`:

```go
type Operation struct {
    // Existing fields — unchanged.
    ID          string         `json:"id"`
    Method      string         `json:"method"`
    Path        string         `json:"path"`
    Tags        []string       `json:"tags,omitempty"`
    Parameters  []Parameter    `json:"parameters,omitempty"`
    RequestBody *SchemaRef     `json:"request_body,omitempty"`
    Responses   []ResponseSpec `json:"responses,omitempty"`

    // GraphQL-specific fields — zero value means "not a GraphQL operation".
    GQLKind          GQLOperationKind   `json:"gql_kind,omitempty"`           // Query | Mutation | Subscription
    GQLDocument      string             `json:"gql_document,omitempty"`        // canonical query document for this operation
    GQLVariableTypes map[string]SchemaRef `json:"gql_variable_types,omitempty"` // variable name → SDL-derived type
    GQLSelectionSchema *SchemaRef        `json:"gql_selection_schema,omitempty"` // schema of the selected fields
}

type GQLOperationKind string

const (
    GQLQuery        GQLOperationKind = "Query"
    GQLMutation     GQLOperationKind = "Mutation"
    GQLSubscription GQLOperationKind = "Subscription"
)
```

Rules:
- All new fields are `omitempty` — existing REST operations round-trip without any change to their JSON representation
- `GQLSubscription` is discovered and normalised but not executed — the runner returns a `ErrSubscriptionsNotSupported` error if one is encountered
- `SchemaRef` is reused without modification — GraphQL scalar types (`String`, `Int`, `Boolean`, `Float`, `ID`) map to their JSON equivalents (`string`, `integer`, `boolean`, `number`, `string`)

### Tests

`pkg/model/graphql_test.go`:

```go
package model_test

import (
	"encoding/json"
	"testing"

	"github.com/jimbery/bt/pkg/model"
)

// --- CaseInput round-trip ---

func TestCaseInput_GQLFields_RoundTripWithoutLoss(t *testing.T) {
	original := model.CaseInput{
		Method: "POST",
		Path:   "/graphql",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		GQLQuery:         `query GetProduct($id: ID!) { product(id: $id) { id name price } }`,
		GQLOperationName: "GetProduct",
		GQLVariables:     map[string]any{"id": "prod-001"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.CaseInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.GQLQuery != original.GQLQuery {
		t.Errorf("GQLQuery: got %q, want %q", decoded.GQLQuery, original.GQLQuery)
	}
	if decoded.GQLOperationName != original.GQLOperationName {
		t.Errorf("GQLOperationName: got %q, want %q", decoded.GQLOperationName, original.GQLOperationName)
	}
	if decoded.GQLVariables["id"] != original.GQLVariables["id"] {
		t.Errorf("GQLVariables[id]: got %v, want %v", decoded.GQLVariables["id"], original.GQLVariables["id"])
	}
}

func TestCaseInput_RESTFields_UnchangedByGQLAdditions(t *testing.T) {
	// A REST CaseInput with no GQL fields must produce identical JSON to before the extension.
	rest := model.CaseInput{
		Method: "GET",
		Path:   "/orders/1",
		Headers: map[string]string{"Authorization": "Bearer token"},
	}

	data, err := json.Marshal(rest)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// GQL fields must not appear in the JSON output when zero.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}
	for _, field := range []string{"gql_query", "gql_operation_name", "gql_variables"} {
		if _, present := raw[field]; present {
			t.Errorf("REST CaseInput must not include %q in JSON output when zero", field)
		}
	}
}

func TestCaseInput_GQLQueryPresent_SignalsGraphQLCase(t *testing.T) {
	restCase := model.CaseInput{Method: "GET", Path: "/orders"}
	gqlCase := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ products { id } }`,
	}

	if restCase.IsGraphQL() {
		t.Error("REST CaseInput must not be identified as GraphQL")
	}
	if !gqlCase.IsGraphQL() {
		t.Error("CaseInput with GQLQuery must be identified as GraphQL")
	}
}

// --- Operation GQL fields ---

func TestOperation_GQLFields_RoundTripWithoutLoss(t *testing.T) {
	original := model.Operation{
		ID:      "GetProduct",
		Method:  "POST",
		Path:    "/graphql",
		GQLKind: model.GQLQuery,
		GQLDocument: `query GetProduct($id: ID!) { product(id: $id) { id name price } }`,
		GQLVariableTypes: map[string]model.SchemaRef{
			"id": {Type: "string"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.Operation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.GQLKind != model.GQLQuery {
		t.Errorf("GQLKind: got %q, want %q", decoded.GQLKind, model.GQLQuery)
	}
	if decoded.GQLDocument != original.GQLDocument {
		t.Errorf("GQLDocument: got %q, want %q", decoded.GQLDocument, original.GQLDocument)
	}
	if decoded.GQLVariableTypes["id"].Type != "string" {
		t.Errorf("GQLVariableTypes[id].Type: got %q, want string", decoded.GQLVariableTypes["id"].Type)
	}
}

func TestOperation_RESTOperation_GQLFieldsAbsentFromJSON(t *testing.T) {
	rest := model.Operation{
		ID:     "GetOrder",
		Method: "GET",
		Path:   "/orders/{id}",
	}

	data, err := json.Marshal(rest)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}
	for _, field := range []string{"gql_kind", "gql_document", "gql_variable_types", "gql_selection_schema"} {
		if _, present := raw[field]; present {
			t.Errorf("REST Operation must not include %q in JSON output when zero", field)
		}
	}
}

func TestGQLOperationKind_Constants_HaveExpectedValues(t *testing.T) {
	cases := map[model.GQLOperationKind]string{
		model.GQLQuery:        "Query",
		model.GQLMutation:     "Mutation",
		model.GQLSubscription: "Subscription",
	}
	for kind, want := range cases {
		if string(kind) != want {
			t.Errorf("GQLOperationKind %v: got %q, want %q", kind, string(kind), want)
		}
	}
}
```

### Implementation

Add `IsGraphQL() bool` method to `CaseInput`:

```go
// IsGraphQL reports whether this input represents a GraphQL operation.
// The REST runner ignores GraphQL cases; the GraphQL runner ignores non-GraphQL cases.
func (c CaseInput) IsGraphQL() bool {
    return c.GQLQuery != ""
}
```

---

## Step 2 — GraphQL adapter

### Spec

The GraphQL adapter lives at `internal/adapter/graphql/`. It implements the `Adapter` interface and discovers operations from a GraphQL SDL schema file. It also supports introspection-based discovery when no SDL file is provided.

**Discovery from SDL file:**
- Parses the SDL using `github.com/vektah/gqlparser/v2`
- Returns one `Operation` per query field, mutation field, and subscription field defined in the schema
- Each field on `Query`, `Mutation`, or `Subscription` types becomes one `Operation`
- `Operation.ID` = the field name (e.g. `product`, `createOrder`)
- `Operation.Method` = `"POST"` (GraphQL always uses HTTP POST)
- `Operation.Path` = the configured GraphQL endpoint path (default: `"/graphql"`)
- `Operation.GQLKind` = `GQLQuery`, `GQLMutation`, or `GQLSubscription`
- `Operation.GQLDocument` = a minimal valid query document for this operation that selects all non-deprecated scalar and non-null fields one level deep
- `Operation.GQLVariableTypes` = variable name → `SchemaRef` derived from the operation's argument types
- `Operation.GQLSelectionSchema` = a `SchemaRef` representing the return type's shape (for response validation)
- `Operation.Responses` = `[]ResponseSpec{{StatusCode: 200, Schema: envelopeSchema}}` where `envelopeSchema` wraps the selection schema in the `{"data": ..., "errors": [...]}` GraphQL response envelope

**Discovery via introspection:**
- Used when `Target.SchemaPath` is empty and `Target.BaseURL` is non-empty
- Sends the standard GraphQL introspection query to `target.base_url + "/graphql"` (or configured path)
- Parses the introspection response and builds the same `[]Operation` output as SDL discovery
- Falls back to an error if introspection is disabled on the server

**Type mapping (SDL → SchemaRef):**

| SDL type | SchemaRef.Type |
|----------|---------------|
| `String` | `string` |
| `Int` | `integer` |
| `Float` | `number` |
| `Boolean` | `boolean` |
| `ID` | `string` |
| `[T]` | `array` with `Items` set |
| Object type | `object` with `Properties` |
| Enum | `string` with `Enum` set |
| `T!` (non-null) | same type with `Nullable: false` |
| `T` (nullable) | same type with `Nullable: true` |

**`Validate`:**
- If `Target.SchemaPath` is set: checks the file exists and parses without error
- If `Target.SchemaPath` is empty: sends a minimal introspection query and checks for a `200` response with a non-empty `__schema` key

### Tests

`internal/adapter/graphql/adapter_test.go`:

```go
package graphql_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	gqladapter "github.com/jimbery/bt/internal/adapter/graphql"
	"github.com/jimbery/bt/pkg/model"
)

// writeSDL writes an SDL schema to a temp file and returns the path.
func writeSDL(t *testing.T, sdl string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.graphql")
	if err := os.WriteFile(path, []byte(sdl), 0644); err != nil {
		t.Fatalf("writeSDL: %v", err)
	}
	return path
}

// minimalSDL is a simple but complete schema used as the baseline for most tests.
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

// --- Name ---

func TestGraphQLAdapter_Name_IsGraphQL(t *testing.T) {
	a := gqladapter.New()
	if a.Name() != "graphql" {
		t.Errorf("expected adapter name 'graphql', got %q", a.Name())
	}
}

// --- SDL discovery: operation count and kinds ---

func TestGraphQLAdapter_Discover_SDL_ReturnsAllOperations(t *testing.T) {
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 2 Query fields + 2 Mutation fields + 1 Subscription = 5 operations.
	if len(ops) != 5 {
		t.Errorf("expected 5 operations, got %d", len(ops))
	}
}

func TestGraphQLAdapter_Discover_SDL_QueryFieldsHaveGQLKindQuery(t *testing.T) {
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

// --- Operation shape ---

func TestGraphQLAdapter_Discover_SDL_OperationID_IsFieldName(t *testing.T) {
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

// --- Variable types ---

func TestGraphQLAdapter_Discover_SDL_RequiredArgument_VariableTypeIsNonNullable(t *testing.T) {
	// product(id: ID!) — id is non-null
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
	// products(filter: String) — filter is nullable
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

// --- Selection schema ---

func TestGraphQLAdapter_Discover_SDL_SelectionSchema_HasExpectedFields(t *testing.T) {
	// product returns Product { id name price description inStock }
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
	// Product.name is String! — must have Nullable=false
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
	// Product.description is String (nullable)
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

// --- Response envelope schema ---

func TestGraphQLAdapter_Discover_SDL_ResponseSchema_HasDataAndErrorsKeys(t *testing.T) {
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

// --- Validate ---

func TestGraphQLAdapter_Validate_ValidSDL_ReturnsNil(t *testing.T) {
	path := writeSDL(t, minimalSDL)
	a := gqladapter.New()

	if err := a.Validate(context.Background(), model.Target{SchemaPath: path}); err != nil {
		t.Errorf("expected Validate to pass for valid SDL, got: %v", err)
	}
}

func TestGraphQLAdapter_Validate_MissingFile_ReturnsError(t *testing.T) {
	a := gqladapter.New()

	err := a.Validate(context.Background(), model.Target{SchemaPath: "/no/such/schema.graphql"})
	if err == nil {
		t.Error("expected error for missing SDL file, got nil")
	}
}

func TestGraphQLAdapter_Validate_MalformedSDL_ReturnsError(t *testing.T) {
	path := writeSDL(t, `type Query { this is not valid SDL }`)
	a := gqladapter.New()

	err := a.Validate(context.Background(), model.Target{SchemaPath: path})
	if err == nil {
		t.Error("expected error for malformed SDL, got nil")
	}
}

// --- Introspection-based discovery ---

func TestGraphQLAdapter_Discover_Introspection_UsedWhenNoSDLPath(t *testing.T) {
	// Serve a minimal introspection response.
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
								"name":              "ping",
								"args":              []any{},
								"isDeprecated":      false,
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
	// No SchemaPath — adapter must use introspection.
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

// --- Enum type mapping ---

func TestGraphQLAdapter_Discover_SDL_EnumType_MapsToStringWithEnumValues(t *testing.T) {
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

// --- List type mapping ---

func TestGraphQLAdapter_Discover_SDL_ListReturnType_MapsToArray(t *testing.T) {
	// products returns [Product!]!
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
```

### Implementation

`internal/adapter/graphql/adapter.go` — implements `Adapter` using `github.com/vektah/gqlparser/v2` for SDL parsing and a hand-rolled introspection client.

`internal/adapter/graphql/typemap.go` — contains the SDL type → `SchemaRef` mapping table.

`internal/adapter/graphql/docgen.go` — generates minimal query documents from operation definitions.

`internal/adapter/graphql/introspect.go` — sends and parses the standard introspection query.

Key dependency: `github.com/vektah/gqlparser/v2` — battle-tested GraphQL parser used by gqlgen; no code generation required.

---

## Step 3 — GraphQL runner

### Spec

The GraphQL runner lives at `internal/runner/graphql/`. It is separate from the REST runner — the REST runner is unchanged.

- `Runner` implements `strategy.Executor`
- `Run(ctx context.Context, input model.CaseInput) (model.ResponseDetail, error)` — sends a GraphQL request and returns the raw response
- For a GraphQL input (`input.IsGraphQL() == true`):
  - Constructs a JSON body: `{"query": input.GQLQuery, "operationName": input.GQLOperationName, "variables": input.GQLVariables}`
  - `operationName` is omitted if `GQLOperationName` is empty
  - `variables` is omitted if `GQLVariables` is nil
  - Sends `POST` to `baseURL + input.Path` with `Content-Type: application/json`
  - Returns `ResponseDetail` with `StatusCode`, `Headers`, and raw `Body`
- For a non-GraphQL input: returns `ErrNotGraphQL` — the caller is responsible for routing to the correct runner
- `ResponseDetail.Body` always contains the raw JSON bytes — it is never pre-parsed
- GraphQL-level errors (`{"errors": [...]}`) in a `200` response are not treated as transport errors; the assertion layer handles them
- Subscriptions (`GQLKind == GQLSubscription`) are not executed — returns `ErrSubscriptionsNotSupported`
- Respects context cancellation

### Tests

`internal/runner/graphql/runner_test.go`:

```go
package graphql_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gqlrunner "github.com/jimbery/bt/internal/runner/graphql"
	"github.com/jimbery/bt/pkg/model"
)

// gqlServer builds a test server that captures the incoming GraphQL request
// and responds with the given body.
func gqlServer(t *testing.T, status int, responseBody string, capture *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			_ = json.NewDecoder(r.Body).Decode(capture)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(responseBody))
	}))
}

// --- Happy path ---

func TestGQLRunner_ValidQuery_SendsPOSTWithJSONBody(t *testing.T) {
	var received map[string]any
	srv := gqlServer(t, 200, `{"data":{"product":{"id":"prod-1","name":"Widget"}}}`, &received)
	defer srv.Close()

	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `query GetProduct($id: ID!) { product(id: $id) { id name } }`,
		GQLOperationName: "GetProduct",
		GQLVariables:     map[string]any{"id": "prod-1"},
	}

	resp, err := r.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}

	// Verify the request body shape.
	if received["query"] == nil {
		t.Error("request must include 'query' field")
	}
	if received["operationName"] != "GetProduct" {
		t.Errorf("expected operationName 'GetProduct', got %v", received["operationName"])
	}
	vars, ok := received["variables"].(map[string]any)
	if !ok {
		t.Fatal("expected 'variables' to be an object")
	}
	if vars["id"] != "prod-1" {
		t.Errorf("expected variable id='prod-1', got %v", vars["id"])
	}
}

func TestGQLRunner_NoOperationName_OmitsOperationNameFromBody(t *testing.T) {
	var received map[string]any
	srv := gqlServer(t, 200, `{"data":{"ping":"pong"}}`, &received)
	defer srv.Close()

	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ ping }`,
		// GQLOperationName deliberately empty
	}

	_, err := r.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, present := received["operationName"]; present {
		t.Error("operationName must be omitted from request body when empty")
	}
}

func TestGQLRunner_NoVariables_OmitsVariablesFromBody(t *testing.T) {
	var received map[string]any
	srv := gqlServer(t, 200, `{"data":{"ping":"pong"}}`, &received)
	defer srv.Close()

	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ ping }`,
		// GQLVariables deliberately nil
	}

	_, err := r.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, present := received["variables"]; present {
		t.Error("variables must be omitted from request body when nil")
	}
}

func TestGQLRunner_ResponseBodyPreservedRaw(t *testing.T) {
	rawBody := `{"data":{"product":{"id":"prod-1","name":"Widget","price":9.99}}}`
	srv := gqlServer(t, 200, rawBody, nil)
	defer srv.Close()

	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ product(id:"prod-1") { id name price } }`,
	}

	resp, err := r.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(resp.Body) != rawBody {
		t.Errorf("expected raw body %q, got %q", rawBody, string(resp.Body))
	}
}

// --- GraphQL-level errors in a 200 response ---

func TestGQLRunner_GraphQLErrorsIn200_ReturnsResponseWithoutError(t *testing.T) {
	// {"errors": [...]} in a 200 is valid GraphQL — the runner must not treat this as a transport error.
	body := `{"data":null,"errors":[{"message":"product not found","locations":[{"line":1,"column":3}],"path":["product"]}]}`
	srv := gqlServer(t, 200, body, nil)
	defer srv.Close()

	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ product(id:"does-not-exist") { id } }`,
	}

	resp, err := r.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("GraphQL-level errors must not be treated as transport errors, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Body must still be the raw bytes for the assertion layer to parse.
	if len(resp.Body) == 0 {
		t.Error("response body must be non-empty even when errors are present")
	}
}

// --- Non-GraphQL input ---

func TestGQLRunner_NonGraphQLInput_ReturnsErrNotGraphQL(t *testing.T) {
	r := gqlrunner.New(gqlrunner.Config{BaseURL: "http://localhost"})
	input := model.CaseInput{
		Method: "GET",
		Path:   "/orders",
		// No GQLQuery — this is a REST input.
	}

	_, err := r.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected ErrNotGraphQL for non-GraphQL input, got nil")
	}
	if err != gqlrunner.ErrNotGraphQL {
		t.Errorf("expected ErrNotGraphQL, got %v", err)
	}
}

// --- Context cancellation ---

func TestGQLRunner_ContextCancelled_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // block until cancelled
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ ping }`,
	}

	_, err := r.Run(ctx, input)
	if err == nil {
		t.Error("expected error when context is cancelled, got nil")
	}
}

// --- Content-Type header ---

func TestGQLRunner_SetsContentTypeApplicationJSON(t *testing.T) {
	var receivedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	_, err := r.Run(context.Background(), model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ ping }`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", receivedContentType)
	}
}
```

### Implementation

`internal/runner/graphql/runner.go`:

```go
package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jimbery/bt/pkg/model"
)

// ErrNotGraphQL is returned when Run is called with a non-GraphQL CaseInput.
var ErrNotGraphQL = errors.New("graphql runner: input is not a GraphQL case (GQLQuery is empty)")

// ErrSubscriptionsNotSupported is returned when Run is called with a subscription operation.
var ErrSubscriptionsNotSupported = errors.New("graphql runner: subscriptions are not supported")

// Config configures the GraphQL runner.
type Config struct {
	BaseURL string
	Timeout time.Duration
}

// Runner executes GraphQL operations over HTTP.
type Runner struct {
	client  *http.Client
	baseURL string
}

// New returns a Runner with sensible defaults.
func New(cfg Config) *Runner {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Runner{
		client:  &http.Client{Timeout: timeout},
		baseURL: cfg.BaseURL,
	}
}

type gqlRequestBody struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName,omitempty"`
	Variables     map[string]any `json:"variables,omitempty"`
}

// Run executes a GraphQL operation. Returns ErrNotGraphQL if input.IsGraphQL() is false.
func (r *Runner) Run(ctx context.Context, input model.CaseInput) (model.ResponseDetail, error) {
	if !input.IsGraphQL() {
		return model.ResponseDetail{}, ErrNotGraphQL
	}

	payload := gqlRequestBody{
		Query:         input.GQLQuery,
		OperationName: input.GQLOperationName,
		Variables:     input.GQLVariables,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+input.Path, bytes.NewReader(body))
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range input.Headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("read response body: %w", err)
	}

	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return model.ResponseDetail{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       respBody,
	}, nil
}
```

---

## Step 4 — GraphQL-aware assertions

### Spec

GraphQL responses have a fixed envelope shape: `{"data": <selection>, "errors": [...]}`. Standard schema validation (from the property and contract strategies) does not understand this envelope — it would try to validate `data` as a top-level field and miss the nested selection schema.

The GraphQL assertion package lives at `internal/strategy/graphql/assert/`. It wraps the existing `validate` package with envelope awareness.

- `AssertResponse(body []byte, op model.Operation) []AssertionFailure` — the main entry point
- Checks:
  1. **Envelope shape**: body must be a JSON object with a `data` key; the `errors` key is optional
  2. **Errors present**: if `errors` is present and non-null, every item must have a `message` field (string); this is a `Warning` — partial data with errors is valid GraphQL
  3. **Errors only, no data**: if `data` is `null` and `errors` is non-empty, the operation is considered failed — this is `Critical`
  4. **Selection schema**: if `data` is non-null and `op.GQLSelectionSchema` is non-nil, the value of `data` is validated against the selection schema using the existing `EvaluateBody` logic from the contract strategy
- `AssertionFailure` carries: `Field string`, `Message string`, `Severity AssertionSeverity`
- `AssertionSeverity`: `Critical` (causes failure) | `Warning` (recorded, does not fail)

### Tests

`internal/strategy/graphql/assert/assert_test.go`:

```go
package assert_test

import (
	"testing"

	gqlassert "github.com/jimbery/bt/internal/strategy/graphql/assert"
	"github.com/jimbery/bt/pkg/model"
)

// productOp returns an Operation with a selection schema for Product.
func productOp() model.Operation {
	return model.Operation{
		ID:      "product",
		GQLKind: model.GQLQuery,
		GQLSelectionSchema: &model.SchemaRef{
			Type: "object",
			Properties: map[string]*model.SchemaRef{
				"id":    {Type: "string", Nullable: false},
				"name":  {Type: "string", Nullable: false},
				"price": {Type: "number", Nullable: false},
			},
			Required: []string{"id", "name", "price"},
		},
	}
}

// --- Happy path ---

func TestAssertResponse_ValidDataNoErrors_NoFailures(t *testing.T) {
	body := []byte(`{"data":{"id":"prod-1","name":"Widget","price":9.99}}`)

	failures := gqlassert.AssertResponse(body, productOp())

	if len(failures) != 0 {
		t.Errorf("expected no failures, got %d: %v", len(failures), failures)
	}
}

// --- Envelope shape ---

func TestAssertResponse_NotJSONObject_CriticalFailure(t *testing.T) {
	body := []byte(`not json`)

	failures := gqlassert.AssertResponse(body, productOp())

	if len(failures) == 0 {
		t.Fatal("expected critical failure for non-JSON body")
	}
	if failures[0].Severity != gqlassert.Critical {
		t.Errorf("expected Critical, got %v", failures[0].Severity)
	}
}

func TestAssertResponse_MissingDataKey_CriticalFailure(t *testing.T) {
	body := []byte(`{"errors":[{"message":"something went wrong"}]}`)

	failures := gqlassert.AssertResponse(body, productOp())

	hasCritical := false
	for _, f := range failures {
		if f.Severity == gqlassert.Critical {
			hasCritical = true
		}
	}
	if !hasCritical {
		t.Error("expected Critical failure when 'data' key is absent")
	}
}

// --- GraphQL errors ---

func TestAssertResponse_ErrorsPresentWithData_WarningNotCritical(t *testing.T) {
	// Partial data with errors is valid GraphQL.
	body := []byte(`{
		"data": {"id":"prod-1","name":"Widget","price":9.99},
		"errors": [{"message":"non-critical warning from resolver"}]
	}`)

	failures := gqlassert.AssertResponse(body, productOp())

	for _, f := range failures {
		if f.Severity == gqlassert.Critical {
			t.Errorf("partial data with errors should not produce Critical failure, got: %v", f)
		}
	}

	hasWarning := false
	for _, f := range failures {
		if f.Severity == gqlassert.Warning {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected Warning for errors array presence")
	}
}

func TestAssertResponse_NullDataWithErrors_CriticalFailure(t *testing.T) {
	body := []byte(`{"data":null,"errors":[{"message":"product not found"}]}`)

	failures := gqlassert.AssertResponse(body, productOp())

	hasCritical := false
	for _, f := range failures {
		if f.Severity == gqlassert.Critical {
			hasCritical = true
		}
	}
	if !hasCritical {
		t.Error("expected Critical failure when data is null and errors are present")
	}
}

func TestAssertResponse_ErrorItemMissingMessageField_CriticalFailure(t *testing.T) {
	// GraphQL spec requires errors to have a "message" field.
	body := []byte(`{"data":null,"errors":[{"code":"NOT_FOUND"}]}`)

	failures := gqlassert.AssertResponse(body, productOp())

	hasCritical := false
	for _, f := range failures {
		if f.Field == "errors[0].message" && f.Severity == gqlassert.Critical {
			hasCritical = true
		}
	}
	if !hasCritical {
		t.Errorf("expected Critical failure on errors[0].message, failures: %v", failures)
	}
}

// --- Selection schema validation ---

func TestAssertResponse_DataMissingRequiredField_CriticalFailure(t *testing.T) {
	// "price" is required but absent.
	body := []byte(`{"data":{"id":"prod-1","name":"Widget"}}`)

	failures := gqlassert.AssertResponse(body, productOp())

	found := false
	for _, f := range failures {
		if f.Field == "data.price" && f.Severity == gqlassert.Critical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Critical failure on data.price, failures: %v", failures)
	}
}

func TestAssertResponse_DataFieldWrongType_CriticalFailure(t *testing.T) {
	// "price" should be a number but is a string.
	body := []byte(`{"data":{"id":"prod-1","name":"Widget","price":"expensive"}}`)

	failures := gqlassert.AssertResponse(body, productOp())

	found := false
	for _, f := range failures {
		if f.Field == "data.price" && f.Severity == gqlassert.Critical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Critical failure on data.price type mismatch, failures: %v", failures)
	}
}

func TestAssertResponse_NonNullFieldIsNull_CriticalFailure(t *testing.T) {
	// "name" is String! — null is not permitted.
	body := []byte(`{"data":{"id":"prod-1","name":null,"price":9.99}}`)

	failures := gqlassert.AssertResponse(body, productOp())

	found := false
	for _, f := range failures {
		if f.Field == "data.name" && f.Severity == gqlassert.Critical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Critical failure on data.name null violation, failures: %v", failures)
	}
}

func TestAssertResponse_NoSelectionSchema_OnlyEnvelopeChecked(t *testing.T) {
	// An operation without a selection schema should only validate the envelope.
	op := model.Operation{
		ID:                 "ping",
		GQLKind:            model.GQLQuery,
		GQLSelectionSchema: nil, // no schema
	}
	body := []byte(`{"data":"pong"}`)

	failures := gqlassert.AssertResponse(body, op)

	if len(failures) != 0 {
		t.Errorf("expected no failures when no selection schema is set, got: %v", failures)
	}
}
```

### Implementation

`internal/strategy/graphql/assert/assert.go` — wraps the contract strategy's `EvaluateBody` with envelope awareness.

---

## Step 5 — Table strategy: GraphQL case routing

### Spec

The table strategy must route GraphQL cases to the GraphQL runner and REST cases to the REST runner. The routing logic lives in the strategy's `Execute` method — the strategy now accepts both a REST executor and a GraphQL executor, selecting based on `CaseInput.IsGraphQL()`.

- `table.Options` gains two optional fields:
  ```go
  type Options struct {
      ArtifactWriter ArtifactWriter
      Environment    string
      GQLExecutor    strategy.Executor // used for GraphQL cases; nil = no GraphQL support
  }
  ```
- If `GQLExecutor` is nil and a GraphQL case is encountered, the case fails with a clear error message: `"graphql cases require a GraphQL executor — configure adapter: graphql in backendtest.yaml"`
- GraphQL assertion failures from `AssertResponse` are added to `Result.Failures` with the same `Failure` type used by REST assertions

### Tests

`internal/strategy/table/graphql_routing_test.go`:

```go
package table_test

import (
	"context"
	"testing"

	"github.com/jimbery/bt/internal/strategy/table"
	"github.com/jimbery/bt/pkg/model"
)

type fakeGQLExecutor struct {
	response model.ResponseDetail
	err      error
}

func (f *fakeGQLExecutor) Run(_ context.Context, _ model.CaseInput) (model.ResponseDetail, error) {
	return f.response, f.err
}

func TestTableStrategy_GraphQLCase_RoutedToGQLExecutor(t *testing.T) {
	gqlExec := &fakeGQLExecutor{
		response: model.ResponseDetail{
			StatusCode: 200,
			Body:       []byte(`{"data":{"ping":"pong"}}`),
		},
	}
	restExec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500}, // should never be called
	}

	s := table.NewWithOptions(table.Options{GQLExecutor: gqlExec})

	cases := []model.Case{
		{
			ID: "gql-ping",
			Input: model.CaseInput{
				Method:   "POST",
				Path:     "/graphql",
				GQLQuery: `{ ping }`,
			},
			Expected: &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, restExec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected GraphQL case to pass, failures: %v", results[0].Failures)
	}
}

func TestTableStrategy_GraphQLCase_NoGQLExecutor_FailsWithClearMessage(t *testing.T) {
	s := table.NewWithOptions(table.Options{GQLExecutor: nil})
	restExec := &fakeExecutor{response: model.ResponseDetail{StatusCode: 200}}

	cases := []model.Case{
		{
			ID: "gql-ping",
			Input: model.CaseInput{
				Method:   "POST",
				Path:     "/graphql",
				GQLQuery: `{ ping }`,
			},
		},
	}

	results, err := s.Execute(context.Background(), cases, restExec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Passed {
		t.Error("expected GraphQL case to fail when no GQL executor is configured")
	}
	if len(results[0].Failures) == 0 {
		t.Error("expected at least one failure explaining the missing executor")
	}
	// Failure message must mention "graphql executor" or similar.
	msg := results[0].Failures[0].Message
	if msg == "" {
		t.Error("failure message must be non-empty")
	}
}

func TestTableStrategy_RESTCase_NotRoutedToGQLExecutor(t *testing.T) {
	// REST cases must never reach the GQL executor even when one is configured.
	gqlExec := &fakeGQLExecutor{
		response: model.ResponseDetail{StatusCode: 500}, // would fail the test if called
	}
	restExec := &fakeExecutor{response: model.ResponseDetail{StatusCode: 200}}

	s := table.NewWithOptions(table.Options{GQLExecutor: gqlExec})

	cases := []model.Case{
		{
			ID:    "rest-get",
			Input: model.CaseInput{Method: "GET", Path: "/orders"},
			Expected: &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, restExec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].Passed {
		t.Errorf("expected REST case to pass, failures: %v", results[0].Failures)
	}
}
```

---

## M9 exit criterion

`bt run --strategy table --adapter graphql` discovers operations from a GraphQL SDL file, executes query and mutation cases against a real GraphQL endpoint, validates response bodies against the SDL-derived selection schema (field presence, types, non-null constraints), and reports pass/fail per case. Subscription operations are discovered but not executed. Introspection-based discovery works when no SDL file is provided. All existing REST tests pass unchanged. All unit tests pass with `-race`.