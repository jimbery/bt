# M2 — OpenAPI Adapter + Table Strategy

This document follows the same structure as M1: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist.

---

## Overview

M2 delivers the first end-to-end path through the platform. By the end of this milestone a team can point `bt` at a real OpenAPI schema, write a table of test cases in YAML, and run them against a live API from the CLI.

The five pieces built here are:

1. **Adapter interface** — the protocol-agnostic boundary all adapters implement
2. **OpenAPI adapter** — discovers and normalises operations from an OpenAPI spec
3. **HTTP runner** — executes a `CaseInput` against a real HTTP endpoint
4. **Table strategy** — loads YAML/JSON/CSV cases and drives the runner
5. **Reporter** — console summary, JSON output, and JUnit XML

Each piece has its own spec, tests, and implementation section. Build and verify each step before moving to the next.

**Exit criterion:** `bt run --strategy table --config backendtest.yaml` executes a table of test cases against a real API and reports pass/fail results to the console. JSON and JUnit outputs are also produced.

---

## Step 1 — Adapter interface

### Spec

- `Adapter` is the contract all protocol adapters implement
- `Discover` takes a `Target` and returns a slice of normalised `Operation` values — it must not execute any test requests
- `Validate` checks that the target is reachable and the schema is parseable, without running any tests
- `Name` returns a short identifier for the adapter (e.g. `"openapi"`)
- The adapter interface lives in `internal/adapter/adapter.go` — it is not protocol-specific

### Tests

`internal/adapter/adapter_test.go`:

```go
package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/yourorg/bt/internal/adapter"
	"github.com/yourorg/bt/pkg/model"
)

// fakeAdapter is a test double that records calls and returns canned values.
type fakeAdapter struct {
	name       string
	operations []model.Operation
	discoverErr error
	validateErr error
	discoverCalled bool
	validateCalled bool
}

func (f *fakeAdapter) Name() string { return f.name }

func (f *fakeAdapter) Discover(ctx context.Context, target model.Target) ([]model.Operation, error) {
	f.discoverCalled = true
	return f.operations, f.discoverErr
}

func (f *fakeAdapter) Validate(ctx context.Context, target model.Target) error {
	f.validateCalled = true
	return f.validateErr
}

func TestAdapter_DiscoverReturnsOperations(t *testing.T) {
	a := &fakeAdapter{
		name: "openapi",
		operations: []model.Operation{
			{ID: "GetOrder", Method: "GET", Path: "/orders/{id}"},
			{ID: "CreateOrder", Method: "POST", Path: "/orders"},
		},
	}

	ops, err := a.Discover(context.Background(), model.Target{Name: "orders-api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a.discoverCalled {
		t.Error("expected Discover to be called")
	}
	if len(ops) != 2 {
		t.Errorf("expected 2 operations, got %d", len(ops))
	}
}

func TestAdapter_DiscoverPropagatesError(t *testing.T) {
	a := &fakeAdapter{
		name:        "openapi",
		discoverErr: errors.New("schema file not found"),
	}

	_, err := a.Discover(context.Background(), model.Target{})
	if err == nil {
		t.Fatal("expected error from Discover")
	}
}

func TestAdapter_ValidateSuccess(t *testing.T) {
	a := &fakeAdapter{name: "openapi"}

	if err := a.Validate(context.Background(), model.Target{Name: "orders-api"}); err != nil {
		t.Errorf("expected Validate to succeed, got: %v", err)
	}
	if !a.validateCalled {
		t.Error("expected Validate to be called")
	}
}

func TestAdapter_ValidatePropagatesError(t *testing.T) {
	a := &fakeAdapter{
		name:        "openapi",
		validateErr: errors.New("schema parse error"),
	}

	if err := a.Validate(context.Background(), model.Target{}); err == nil {
		t.Fatal("expected error from Validate")
	}
}

func TestAdapter_NameIsNonEmpty(t *testing.T) {
	a := &fakeAdapter{name: "openapi"}
	if a.Name() == "" {
		t.Error("adapter Name must not be empty")
	}
}
```

### Implementation

`internal/adapter/adapter.go`:

```go
package adapter

import (
	"context"

	"github.com/yourorg/bt/pkg/model"
)

// Adapter is the contract all protocol adapters must implement.
// Adapters own discovery and normalisation only — execution policy
// and test logic live in the engine and strategies.
type Adapter interface {
	// Name returns a short identifier for this adapter, e.g. "openapi".
	Name() string

	// Discover parses the schema referenced by target and returns a
	// normalised slice of Operations. It must not make test requests.
	Discover(ctx context.Context, target model.Target) ([]model.Operation, error)

	// Validate checks that the target schema is parseable and the
	// base URL is reachable. It must not run any test cases.
	Validate(ctx context.Context, target model.Target) error
}
```

Run the tests:

```bash
go test ./internal/adapter/... -race -v
```

---

## Step 2 — OpenAPI adapter

### Spec

- The OpenAPI adapter parses a spec from a file path or URL
- It supports OpenAPI 3.x specs in YAML or JSON format
- `Discover` returns one `Operation` per path+method combination found in the spec
- Each `Operation` is populated with: ID (operationId or generated), method, path, tags, parameters (path, query, header), request body schema, and response schemas
- `SchemaRef` fields — `type`, `format`, `properties`, `items`, `required`, `nullable`, `enum`, `oneOf`, `anyOf` — are normalised from the OpenAPI schema object
- Circular references in schemas must not cause infinite loops — they are broken by returning a `SchemaRef` with only a `type` annotation
- `Validate` checks that the file exists and is valid OpenAPI 3.x — it does not make HTTP calls
- Operations without an `operationId` get a generated ID in the form `METHOD_path_segments` (e.g. `GET_orders_id`)

### Tests

`internal/adapter/openapi/openapi_test.go`:

```go
package openapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourorg/bt/internal/adapter/openapi"
	"github.com/yourorg/bt/pkg/model"
)

// writeSpec writes an OpenAPI spec to a temp file and returns the path.
func writeSpec(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimalSpec = `
openapi: "3.0.3"
info:
  title: Orders API
  version: "1.0.0"
paths:
  /orders:
    get:
      operationId: ListOrders
      summary: List all orders
      responses:
        "200":
          description: OK
          content:
            application/json:
              schema:
                type: array
                items:
                  type: object
    post:
      operationId: CreateOrder
      summary: Create an order
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [amount]
              properties:
                amount:
                  type: integer
                  format: int64
                note:
                  type: string
                  nullable: true
      responses:
        "201":
          description: Created
        "400":
          description: Bad request
  /orders/{id}:
    get:
      operationId: GetOrder
      summary: Get an order by ID
      parameters:
        - name: id
          in: path
          required: true
          schema:
            type: string
      responses:
        "200":
          description: OK
        "404":
          description: Not found
`

func TestOpenAPIAdapter_Name(t *testing.T) {
	a := openapi.New()
	if a.Name() != "openapi" {
		t.Errorf("expected name %q, got %q", "openapi", a.Name())
	}
}

func TestOpenAPIAdapter_Discover_ReturnsAllOperations(t *testing.T) {
	path := writeSpec(t, minimalSpec)
	a := openapi.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expect 3 operations: ListOrders, CreateOrder, GetOrder
	if len(ops) != 3 {
		t.Errorf("expected 3 operations, got %d", len(ops))
	}
}

func TestOpenAPIAdapter_Discover_OperationIDs(t *testing.T) {
	path := writeSpec(t, minimalSpec)
	a := openapi.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ids := make(map[string]bool)
	for _, op := range ops {
		ids[op.ID] = true
	}

	for _, want := range []string{"ListOrders", "CreateOrder", "GetOrder"} {
		if !ids[want] {
			t.Errorf("expected operation %q not found in discovered operations", want)
		}
	}
}

func TestOpenAPIAdapter_Discover_MethodAndPath(t *testing.T) {
	path := writeSpec(t, minimalSpec)
	a := openapi.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	found := false
	for _, op := range ops {
		if op.ID == "CreateOrder" {
			found = true
			if op.Method != "POST" {
				t.Errorf("CreateOrder method: got %q, want POST", op.Method)
			}
			if op.Path != "/orders" {
				t.Errorf("CreateOrder path: got %q, want /orders", op.Path)
			}
		}
	}
	if !found {
		t.Error("CreateOrder not found in discovered operations")
	}
}

func TestOpenAPIAdapter_Discover_RequestBodySchema(t *testing.T) {
	path := writeSpec(t, minimalSpec)
	a := openapi.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var createOrder *model.Operation
	for i := range ops {
		if ops[i].ID == "CreateOrder" {
			createOrder = &ops[i]
			break
		}
	}
	if createOrder == nil {
		t.Fatal("CreateOrder not found")
	}
	if createOrder.RequestBody == nil {
		t.Fatal("expected non-nil RequestBody on CreateOrder")
	}
	if createOrder.RequestBody.Type != "object" {
		t.Errorf("RequestBody.Type: got %q, want object", createOrder.RequestBody.Type)
	}
	if _, ok := createOrder.RequestBody.Properties["amount"]; !ok {
		t.Error("expected 'amount' in RequestBody.Properties")
	}
	amountSchema := createOrder.RequestBody.Properties["amount"]
	if amountSchema.Type != "integer" {
		t.Errorf("amount type: got %q, want integer", amountSchema.Type)
	}
	if amountSchema.Format != "int64" {
		t.Errorf("amount format: got %q, want int64", amountSchema.Format)
	}
}

func TestOpenAPIAdapter_Discover_NullableField(t *testing.T) {
	path := writeSpec(t, minimalSpec)
	a := openapi.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, op := range ops {
		if op.ID == "CreateOrder" {
			note, ok := op.RequestBody.Properties["note"]
			if !ok {
				t.Fatal("expected 'note' field in CreateOrder request body")
			}
			if !note.Nullable {
				t.Error("expected 'note' field to be nullable")
			}
		}
	}
}

func TestOpenAPIAdapter_Discover_PathParameter(t *testing.T) {
	path := writeSpec(t, minimalSpec)
	a := openapi.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, op := range ops {
		if op.ID == "GetOrder" {
			if len(op.Parameters) != 1 {
				t.Fatalf("expected 1 parameter on GetOrder, got %d", len(op.Parameters))
			}
			p := op.Parameters[0]
			if p.Name != "id" {
				t.Errorf("parameter name: got %q, want id", p.Name)
			}
			if p.In != "path" {
				t.Errorf("parameter in: got %q, want path", p.In)
			}
			if !p.Required {
				t.Error("expected path parameter 'id' to be required")
			}
		}
	}
}

func TestOpenAPIAdapter_Discover_ResponseSpecs(t *testing.T) {
	path := writeSpec(t, minimalSpec)
	a := openapi.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, op := range ops {
		if op.ID == "CreateOrder" {
			if len(op.Responses) != 2 {
				t.Errorf("expected 2 responses on CreateOrder, got %d", len(op.Responses))
			}
			codes := make(map[int]bool)
			for _, r := range op.Responses {
				codes[r.StatusCode] = true
			}
			if !codes[201] {
				t.Error("expected 201 response on CreateOrder")
			}
			if !codes[400] {
				t.Error("expected 400 response on CreateOrder")
			}
		}
	}
}

func TestOpenAPIAdapter_Discover_GeneratedOperationID(t *testing.T) {
	// When operationId is absent, a deterministic ID must be generated.
	spec := `
openapi: "3.0.3"
info:
  title: Test
  version: "1.0.0"
paths:
  /items/{id}:
    get:
      summary: Get item
      responses:
        "200":
          description: OK
`
	path := writeSpec(t, spec)
	a := openapi.New()

	ops, err := a.Discover(context.Background(), model.Target{SchemaPath: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("expected 1 operation, got %d", len(ops))
	}
	if ops[0].ID == "" {
		t.Error("expected a generated operation ID, got empty string")
	}
}

func TestOpenAPIAdapter_Discover_MissingFile(t *testing.T) {
	a := openapi.New()
	_, err := a.Discover(context.Background(), model.Target{SchemaPath: "/nonexistent/openapi.yaml"})
	if err == nil {
		t.Fatal("expected error for missing schema file")
	}
}

func TestOpenAPIAdapter_Validate_ValidSpec(t *testing.T) {
	path := writeSpec(t, minimalSpec)
	a := openapi.New()

	if err := a.Validate(context.Background(), model.Target{SchemaPath: path}); err != nil {
		t.Errorf("expected Validate to succeed on valid spec, got: %v", err)
	}
}

func TestOpenAPIAdapter_Validate_InvalidSpec(t *testing.T) {
	path := writeSpec(t, "this is not valid openapi yaml: :::")
	a := openapi.New()

	if err := a.Validate(context.Background(), model.Target{SchemaPath: path}); err == nil {
		t.Error("expected Validate to fail on invalid spec")
	}
}

func TestOpenAPIAdapter_Validate_MissingFile(t *testing.T) {
	a := openapi.New()
	if err := a.Validate(context.Background(), model.Target{SchemaPath: "/nonexistent/openapi.yaml"}); err == nil {
		t.Error("expected Validate to fail for missing file")
	}
}
```

### Implementation

Install the OpenAPI parsing library:

```bash
go get github.com/pb33f/libopenapi@latest
go mod tidy
```

`internal/adapter/openapi/openapi.go`:

```go
package openapi

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
	"github.com/yourorg/bt/internal/adapter"
	"github.com/yourorg/bt/pkg/model"
)

// Adapter implements adapter.Adapter for OpenAPI 3.x specs.
type Adapter struct{}

// New returns a new OpenAPI adapter.
func New() adapter.Adapter {
	return &Adapter{}
}

func (a *Adapter) Name() string { return "openapi" }

func (a *Adapter) Validate(_ context.Context, target model.Target) error {
	data, err := os.ReadFile(target.SchemaPath)
	if err != nil {
		return fmt.Errorf("cannot read schema: %w", err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return fmt.Errorf("cannot parse schema: %w", err)
	}
	_, errs := doc.BuildV3Model()
	if len(errs) > 0 {
		return fmt.Errorf("invalid OpenAPI spec: %v", errs[0])
	}
	return nil
}

func (a *Adapter) Discover(_ context.Context, target model.Target) ([]model.Operation, error) {
	data, err := os.ReadFile(target.SchemaPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read schema: %w", err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("cannot parse schema: %w", err)
	}
	v3model, errs := doc.BuildV3Model()
	if len(errs) > 0 {
		return nil, fmt.Errorf("invalid OpenAPI spec: %v", errs[0])
	}

	var ops []model.Operation
	for path, item := range v3model.Model.Paths.PathItems.FromOldest() {
		for method, op := range operationsFromPathItem(item) {
			ops = append(ops, normaliseOperation(method, path, op))
		}
	}
	return ops, nil
}

// operationsFromPathItem returns a map of HTTP method → operation for a path item.
func operationsFromPathItem(item *v3.PathItem) map[string]*v3.Operation {
	m := make(map[string]*v3.Operation)
	if item.Get != nil    { m["GET"] = item.Get }
	if item.Post != nil   { m["POST"] = item.Post }
	if item.Put != nil    { m["PUT"] = item.Put }
	if item.Patch != nil  { m["PATCH"] = item.Patch }
	if item.Delete != nil { m["DELETE"] = item.Delete }
	if item.Head != nil   { m["HEAD"] = item.Head }
	if item.Options != nil { m["OPTIONS"] = item.Options }
	return m
}

// normaliseOperation converts an OpenAPI operation into the shared model.Operation type.
func normaliseOperation(method, path string, op *v3.Operation) model.Operation {
	id := op.OperationId
	if id == "" {
		id = generateID(method, path)
	}

	out := model.Operation{
		ID:     id,
		Method: method,
		Path:   path,
		Tags:   op.Tags,
	}

	for _, p := range op.Parameters {
		param := model.Parameter{
			Name:     p.Name,
			In:       p.In,
			Required: p.Required != nil && *p.Required,
		}
		if p.Schema != nil {
			schema := p.Schema.Schema()
			param.Schema = normaliseSchema(schema, 0)
		}
		out.Parameters = append(out.Parameters, param)
	}

	if op.RequestBody != nil {
		for _, mediaType := range op.RequestBody.Content.FromOldest() {
			if mediaType.Schema != nil {
				out.RequestBody = normaliseSchema(mediaType.Schema.Schema(), 0)
			}
			break // use first content type
		}
	}

	if op.Responses != nil {
		for code, resp := range op.Responses.Codes.FromOldest() {
			statusCode := parseStatusCode(code)
			rs := model.ResponseSpec{StatusCode: statusCode}
			if resp.Content != nil {
				for _, mediaType := range resp.Content.FromOldest() {
					if mediaType.Schema != nil {
						rs.Schema = normaliseSchema(mediaType.Schema.Schema(), 0)
					}
					break
				}
			}
			out.Responses = append(out.Responses, rs)
		}
	}

	return out
}

const maxSchemaDepth = 10

// normaliseSchema converts an OpenAPI schema into the shared model.SchemaRef type.
// depth guards against circular references.
func normaliseSchema(s interface{ GetType() []string }, depth int) *model.SchemaRef {
	if s == nil || depth > maxSchemaDepth {
		return &model.SchemaRef{}
	}

	// Use type assertion to access the full schema via libopenapi's high-level model.
	type fullSchema interface {
		GetType() []string
		GetFormat() string
		GetNullable() *bool
		GetRequired() []string
		GetEnum() []interface{}
	}

	ref := &model.SchemaRef{}

	if fs, ok := s.(fullSchema); ok {
		types := fs.GetType()
		if len(types) > 0 {
			ref.Type = types[0]
		}
		ref.Format = fs.GetFormat()
		if n := fs.GetNullable(); n != nil {
			ref.Nullable = *n
		}
		ref.Required = fs.GetRequired()
	}

	return ref
}

// generateID produces a deterministic operation ID from method and path
// when the spec does not provide an operationId.
func generateID(method, path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	segments := make([]string, 0, len(parts))
	for _, p := range parts {
		clean := strings.NewReplacer("{", "", "}", "").Replace(p)
		if clean != "" {
			segments = append(segments, clean)
		}
	}
	return strings.ToUpper(method) + "_" + strings.Join(segments, "_")
}

func parseStatusCode(code string) int {
	var n int
	fmt.Sscanf(code, "%d", &n)
	return n
}
```

> **Note:** `libopenapi`'s high-level model API evolves — check the library docs if any method signatures differ. The normalisation approach above uses the high-level v3 model. A more complete `normaliseSchema` implementation for M4 (property testing) will need to handle `properties`, `items`, `oneOf`, and `anyOf` — that is a planned spike at the start of M4 per ADR-006.

Run the tests:

```bash
go test ./internal/adapter/openapi/... -race -v
```

---

## Step 3 — HTTP runner

### Spec

- `Runner` executes a `CaseInput` against a real HTTP endpoint and returns a `ResponseDetail`
- It constructs the full URL from a base URL and the case path
- It sets request headers from `CaseInput.Headers`
- It serialises `CaseInput.Body` as JSON if non-nil
- It respects `context.Context` cancellation — if the context is cancelled the request is aborted
- It returns the response status code, headers, and raw body
- It does not evaluate invariants or assertions — that is the strategy's responsibility
- Timeouts are configurable; default is 30 seconds per request

### Tests

`internal/runner/runner_test.go`:

```go
package runner_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yourorg/bt/internal/runner"
	"github.com/yourorg/bt/pkg/model"
)

func TestRunner_GetRequest_ReturnsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id":"ord-1"}`))
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	resp, err := r.Run(context.Background(), model.CaseInput{
		Method: "GET",
		Path:   "/orders/1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode: got %d, want 200", resp.StatusCode)
	}
	if string(resp.Body) != `{"id":"ord-1"}` {
		t.Errorf("Body: got %s, want %s", resp.Body, `{"id":"ord-1"}`)
	}
}

func TestRunner_PostRequest_SendsJSONBody(t *testing.T) {
	var received map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	_, err := r.Run(context.Background(), model.CaseInput{
		Method: "POST",
		Path:   "/orders",
		Body:   map[string]any{"amount": 100},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received["amount"] == nil {
		t.Error("expected body to be received by server")
	}
}

func TestRunner_SetsRequestHeaders(t *testing.T) {
	var receivedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Idempotency-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	_, err := r.Run(context.Background(), model.CaseInput{
		Method:  "POST",
		Path:    "/orders",
		Headers: map[string]string{"X-Idempotency-Key": "abc-123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedHeader != "abc-123" {
		t.Errorf("X-Idempotency-Key: got %q, want %q", receivedHeader, "abc-123")
	}
}

func TestRunner_ReturnsResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-xyz")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	resp, err := r.Run(context.Background(), model.CaseInput{Method: "GET", Path: "/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers["X-Request-Id"] != "req-xyz" {
		t.Errorf("X-Request-Id: got %q, want req-xyz", resp.Headers["X-Request-Id"])
	}
}

func TestRunner_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block long enough for the context to be cancelled.
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := r.Run(ctx, model.CaseInput{Method: "GET", Path: "/"})
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
}

func TestRunner_Non2xxStatusIsNotAnError(t *testing.T) {
	// The runner returns non-2xx responses without error.
	// It is the invariant layer's job to decide if a 404 or 500 is a failure.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	resp, err := r.Run(context.Background(), model.CaseInput{Method: "GET", Path: "/missing"})
	if err != nil {
		t.Fatalf("unexpected error for 404: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("StatusCode: got %d, want 404", resp.StatusCode)
	}
}

func TestRunner_QueryParams_AppendedToURL(t *testing.T) {
	var receivedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	_, err := r.Run(context.Background(), model.CaseInput{
		Method: "GET",
		Path:   "/orders",
		Query:  map[string]string{"status": "pending"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedQuery != "status=pending" {
		t.Errorf("query string: got %q, want %q", receivedQuery, "status=pending")
	}
}
```

### Implementation

`internal/runner/runner.go`:

```go
package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/yourorg/bt/pkg/model"
)

// Config holds runner configuration.
type Config struct {
	BaseURL string
	Timeout time.Duration
}

// Runner executes CaseInputs against a real HTTP endpoint.
type Runner struct {
	client  *http.Client
	baseURL string
}

// New returns a new Runner with the given config.
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

// Run executes a single CaseInput and returns the ResponseDetail.
// Non-2xx responses are not treated as errors — invariant evaluation
// decides whether a given status code is a failure.
func (r *Runner) Run(ctx context.Context, input model.CaseInput) (model.ResponseDetail, error) {
	u, err := url.Parse(r.baseURL + input.Path)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("invalid URL: %w", err)
	}

	if len(input.Query) > 0 {
		q := u.Query()
		for k, v := range input.Query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	var bodyReader io.Reader
	if input.Body != nil {
		data, err := json.Marshal(input.Body)
		if err != nil {
			return model.ResponseDetail{}, fmt.Errorf("cannot marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, input.Method, u.String(), bodyReader)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("cannot build request: %w", err)
	}

	if input.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range input.Headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("cannot read response body: %w", err)
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
		Body:       body,
	}, nil
}
```

Run the tests:

```bash
go test ./internal/runner/... -race -v
```

---

## Step 4 — Table strategy

### Spec

- The table strategy loads test cases from a YAML file
- Each case in the YAML maps to a `model.Case` with an operation ID, input, and optional expectation
- `Plan` reads the case file and returns the cases — it must not make network calls
- `Execute` runs each case through the `Executor` and evaluates built-in assertions
- Built-in assertions for M2: status code match, required response headers present
- A case passes if all its assertions pass; it fails otherwise, with a `Failure` per failing assertion
- Cases without an expectation are executed and recorded but always pass assertion checks
- The case file path comes from `Spec.Config["file"]`

### Tests

`internal/strategy/table/table_test.go`:

```go
package table_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourorg/bt/internal/strategy"
	"github.com/yourorg/bt/internal/strategy/table"
	"github.com/yourorg/bt/pkg/model"
)

// fakeExecutor returns a canned ResponseDetail.
type fakeExecutor struct {
	response model.ResponseDetail
	err      error
}

func (f *fakeExecutor) Run(_ context.Context, _ model.CaseInput) (model.ResponseDetail, error) {
	return f.response, f.err
}

func writeCaseFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cases.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimalCases = `
cases:
  - id: get-order-200
    operation_id: GetOrder
    input:
      method: GET
      path: /orders/1
    expected:
      status_code: 200
  - id: create-order-201
    operation_id: CreateOrder
    input:
      method: POST
      path: /orders
      headers:
        Content-Type: application/json
      body:
        amount: 100
    expected:
      status_code: 201
`

func TestTableStrategy_Plan_LoadsCases(t *testing.T) {
	path := writeCaseFile(t, minimalCases)

	s := table.New()
	spec := strategy.Spec{
		Kind:   strategy.KindTable,
		Config: map[string]any{"file": path},
	}

	cases, err := s.Plan(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Plan returned unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Errorf("expected 2 cases, got %d", len(cases))
	}
}

func TestTableStrategy_Plan_CaseFields(t *testing.T) {
	path := writeCaseFile(t, minimalCases)

	s := table.New()
	spec := strategy.Spec{
		Kind:   strategy.KindTable,
		Config: map[string]any{"file": path},
	}

	cases, err := s.Plan(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Plan returned unexpected error: %v", err)
	}

	c := cases[0]
	if c.ID != "get-order-200" {
		t.Errorf("ID: got %q, want get-order-200", c.ID)
	}
	if c.OperationID != "GetOrder" {
		t.Errorf("OperationID: got %q, want GetOrder", c.OperationID)
	}
	if c.Input.Method != "GET" {
		t.Errorf("Input.Method: got %q, want GET", c.Input.Method)
	}
	if c.Input.Path != "/orders/1" {
		t.Errorf("Input.Path: got %q, want /orders/1", c.Input.Path)
	}
	if c.Expected == nil {
		t.Fatal("expected non-nil Expected")
	}
	if c.Expected.StatusCode != 200 {
		t.Errorf("Expected.StatusCode: got %d, want 200", c.Expected.StatusCode)
	}
}

func TestTableStrategy_Plan_MissingFile(t *testing.T) {
	s := table.New()
	spec := strategy.Spec{
		Kind:   strategy.KindTable,
		Config: map[string]any{"file": "/nonexistent/cases.yaml"},
	}

	_, err := s.Plan(context.Background(), spec, nil)
	if err == nil {
		t.Fatal("expected error for missing case file")
	}
}

func TestTableStrategy_Plan_MissingFileConfig(t *testing.T) {
	s := table.New()
	spec := strategy.Spec{
		Kind:   strategy.KindTable,
		Config: map[string]any{},
	}

	_, err := s.Plan(context.Background(), spec, nil)
	if err == nil {
		t.Fatal("expected error when file config key is missing")
	}
}

func TestTableStrategy_Execute_PassesOnStatusMatch(t *testing.T) {
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 200},
	}

	cases := []model.Case{
		{
			ID:          "get-order-200",
			OperationID: "GetOrder",
			Input:       model.CaseInput{Method: "GET", Path: "/orders/1"},
			Expected:    &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected result to pass, failures: %v", results[0].Failures)
	}
}

func TestTableStrategy_Execute_FailsOnStatusMismatch(t *testing.T) {
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}

	cases := []model.Case{
		{
			ID:          "get-order-200",
			OperationID: "GetOrder",
			Input:       model.CaseInput{Method: "GET", Path: "/orders/1"},
			Expected:    &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if results[0].Passed {
		t.Error("expected result to fail on status mismatch")
	}
	if len(results[0].Failures) == 0 {
		t.Error("expected at least one failure recorded")
	}
	if results[0].Failures[0].Invariant != "status_code" {
		t.Errorf("Invariant: got %q, want status_code", results[0].Failures[0].Invariant)
	}
}

func TestTableStrategy_Execute_PassesWithNoExpectation(t *testing.T) {
	// A case with no expectation is always recorded as passed.
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}

	cases := []model.Case{
		{
			ID:          "fire-and-forget",
			OperationID: "CreateOrder",
			Input:       model.CaseInput{Method: "POST", Path: "/orders"},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !results[0].Passed {
		t.Error("expected case with no expectation to pass")
	}
}

func TestTableStrategy_Execute_RecordsRequestAndResponse(t *testing.T) {
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{
			StatusCode: 200,
			Body:       []byte(`{"id":"ord-1"}`),
		},
	}

	cases := []model.Case{
		{
			ID:    "get-order",
			Input: model.CaseInput{Method: "GET", Path: "/orders/1"},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if results[0].Response.StatusCode != 200 {
		t.Errorf("Response.StatusCode: got %d, want 200", results[0].Response.StatusCode)
	}
}

func TestTableStrategy_Name(t *testing.T) {
	s := table.New()
	if s.Name() != strategy.KindTable {
		t.Errorf("Name: got %q, want %q", s.Name(), strategy.KindTable)
	}
}
```

### Implementation

`internal/strategy/table/table.go`:

```go
package table

import (
	"context"
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/yourorg/bt/internal/strategy"
	"github.com/yourorg/bt/pkg/model"
)

// caseFile is the on-disk format for a table test case file.
type caseFile struct {
	Cases []caseEntry `yaml:"cases"`
}

type caseEntry struct {
	ID          string              `yaml:"id"`
	OperationID string              `yaml:"operation_id"`
	Input       caseInputEntry      `yaml:"input"`
	Expected    *caseExpectedEntry  `yaml:"expected"`
}

type caseInputEntry struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
	Query   map[string]string `yaml:"query"`
	Body    any               `yaml:"body"`
}

type caseExpectedEntry struct {
	StatusCode int               `yaml:"status_code"`
	Headers    map[string]string `yaml:"headers"`
}

// Strategy implements strategy.Strategy for table-driven testing.
type Strategy struct{}

// New returns a new table Strategy.
func New() strategy.Strategy {
	return &Strategy{}
}

func (s *Strategy) Name() strategy.Kind { return strategy.KindTable }

// Plan loads cases from the YAML file specified in spec.Config["file"].
// It does not make network calls.
func (s *Strategy) Plan(_ context.Context, spec strategy.Spec, _ []model.Operation) ([]model.Case, error) {
	filePath, ok := spec.Config["file"].(string)
	if !ok || filePath == "" {
		return nil, errors.New("table strategy requires config.file to be set")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read case file: %w", err)
	}

	var cf caseFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("cannot parse case file: %w", err)
	}

	cases := make([]model.Case, 0, len(cf.Cases))
	for _, entry := range cf.Cases {
		c := model.Case{
			ID:          entry.ID,
			OperationID: entry.OperationID,
			Input: model.CaseInput{
				Method:  entry.Input.Method,
				Path:    entry.Input.Path,
				Headers: entry.Input.Headers,
				Query:   entry.Input.Query,
				Body:    entry.Input.Body,
			},
		}
		if entry.Expected != nil {
			c.Expected = &model.CaseExpectation{
				StatusCode: entry.Expected.StatusCode,
				Headers:    entry.Expected.Headers,
			}
		}
		cases = append(cases, c)
	}

	return cases, nil
}

// Execute runs each case through the executor and evaluates assertions.
func (s *Strategy) Execute(ctx context.Context, cases []model.Case, exec strategy.Executor) ([]model.Result, error) {
	results := make([]model.Result, 0, len(cases))

	for _, c := range cases {
		resp, err := exec.Run(ctx, c.Input)
		if err != nil {
			return nil, fmt.Errorf("case %q: executor error: %w", c.ID, err)
		}

		result := model.Result{
			CaseID:     c.ID,
			StatusCode: resp.StatusCode,
			Response:   resp,
			Request: model.RequestDetail{
				Method: c.Input.Method,
			},
		}

		var failures []model.Failure

		if c.Expected != nil {
			if c.Expected.StatusCode != 0 && resp.StatusCode != c.Expected.StatusCode {
				failures = append(failures, model.Failure{
					Invariant: "status_code",
					Message:   fmt.Sprintf("expected status %d, got %d", c.Expected.StatusCode, resp.StatusCode),
					Expected:  c.Expected.StatusCode,
					Actual:    resp.StatusCode,
				})
			}

			for header, want := range c.Expected.Headers {
				got := resp.Headers[header]
				if got != want {
					failures = append(failures, model.Failure{
						Invariant: "response_header",
						Message:   fmt.Sprintf("header %q: expected %q, got %q", header, want, got),
						Expected:  want,
						Actual:    got,
					})
				}
			}
		}

		result.Failures = failures
		result.Passed = len(failures) == 0
		results = append(results, result)
	}

	return results, nil
}
```

Install the YAML library:

```bash
go get gopkg.in/yaml.v3
go mod tidy
```

Run the tests:

```bash
go test ./internal/strategy/table/... -race -v
```

---

## Step 5 — Reporter

### Spec

- The reporter takes a slice of `Result` values and writes output in the requested format
- Console output shows a summary line per case (PASS/FAIL, case ID, status code, duration) and a final summary (total, passed, failed)
- JSON output writes a single JSON object with a `results` array and a `summary` block
- JUnit output writes a `<testsuites>` XML document compatible with CI pipeline test dashboards
- Each reporter implements a common `Reporter` interface
- Reporters write to an `io.Writer` so output can be captured in tests

### Tests

`internal/report/report_test.go`:

```go
package report_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/yourorg/bt/internal/report"
	"github.com/yourorg/bt/pkg/model"
)

var sampleResults = []model.Result{
	{
		CaseID:     "get-order-200",
		Passed:     true,
		StatusCode: 200,
		Duration:   12 * time.Millisecond,
	},
	{
		CaseID:     "create-order-201",
		Passed:     false,
		StatusCode: 500,
		Duration:   8 * time.Millisecond,
		Failures: []model.Failure{
			{Invariant: "status_code", Message: "expected 201, got 500"},
		},
	},
}

// Console reporter

func TestConsoleReporter_ContainsCaseIDs(t *testing.T) {
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "get-order-200") {
		t.Error("expected output to contain case ID get-order-200")
	}
	if !strings.Contains(output, "create-order-201") {
		t.Error("expected output to contain case ID create-order-201")
	}
}

func TestConsoleReporter_ShowsPassFail(t *testing.T) {
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "PASS") {
		t.Error("expected output to contain PASS")
	}
	if !strings.Contains(output, "FAIL") {
		t.Error("expected output to contain FAIL")
	}
}

func TestConsoleReporter_ShowsSummary(t *testing.T) {
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	// Should contain total, passed, failed counts.
	if !strings.Contains(output, "2") {
		t.Error("expected output to contain total count")
	}
}

// JSON reporter

func TestJSONReporter_ValidJSON(t *testing.T) {
	var buf bytes.Buffer
	r := report.NewJSON(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestJSONReporter_ContainsResults(t *testing.T) {
	var buf bytes.Buffer
	r := report.NewJSON(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out struct {
		Results []map[string]any `json:"results"`
		Summary map[string]any   `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("cannot unmarshal JSON output: %v", err)
	}
	if len(out.Results) != 2 {
		t.Errorf("expected 2 results in JSON output, got %d", len(out.Results))
	}
	if out.Summary == nil {
		t.Error("expected non-nil summary in JSON output")
	}
}

// JUnit reporter

func TestJUnitReporter_ValidXML(t *testing.T) {
	var buf bytes.Buffer
	r := report.NewJUnit(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var suites report.JUnitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &suites); err != nil {
		t.Fatalf("output is not valid JUnit XML: %v", err)
	}
}

func TestJUnitReporter_ContainsTestCases(t *testing.T) {
	var buf bytes.Buffer
	r := report.NewJUnit(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var suites report.JUnitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &suites); err != nil {
		t.Fatalf("cannot unmarshal JUnit XML: %v", err)
	}
	if len(suites.Suites) == 0 {
		t.Fatal("expected at least one test suite")
	}
	suite := suites.Suites[0]
	if suite.Tests != 2 {
		t.Errorf("expected 2 tests, got %d", suite.Tests)
	}
	if suite.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", suite.Failures)
	}
}
```

### Implementation

`internal/report/reporter.go`:

```go
package report

import (
	"io"

	"github.com/yourorg/bt/pkg/model"
)

// Reporter writes test results to an io.Writer in a specific format.
type Reporter interface {
	Write(results []model.Result) error
}

// summary computes aggregate counts across a result set.
type summary struct {
	Total   int
	Passed  int
	Failed  int
}

func summarise(results []model.Result) summary {
	s := summary{Total: len(results)}
	for _, r := range results {
		if r.Passed {
			s.Passed++
		} else {
			s.Failed++
		}
	}
	return s
}
```

`internal/report/console.go`:

```go
package report

import (
	"fmt"
	"io"

	"github.com/yourorg/bt/pkg/model"
)

type consoleReporter struct{ w io.Writer }

// NewConsole returns a Reporter that writes a human-readable summary.
func NewConsole(w io.Writer) Reporter { return &consoleReporter{w: w} }

func (r *consoleReporter) Write(results []model.Result) error {
	for _, res := range results {
		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(r.w, "  %s  %s  (HTTP %d, %s)\n",
			status, res.CaseID, res.StatusCode, res.Duration)
		for _, f := range res.Failures {
			fmt.Fprintf(r.w, "       %s: %s\n", f.Invariant, f.Message)
		}
	}

	s := summarise(results)
	fmt.Fprintf(r.w, "\n%d tests run: %d passed, %d failed\n", s.Total, s.Passed, s.Failed)
	return nil
}
```

`internal/report/json.go`:

```go
package report

import (
	"encoding/json"
	"io"

	"github.com/yourorg/bt/pkg/model"
)

type jsonReporter struct{ w io.Writer }

// NewJSON returns a Reporter that writes a JSON report.
func NewJSON(w io.Writer) Reporter { return &jsonReporter{w: w} }

func (r *jsonReporter) Write(results []model.Result) error {
	s := summarise(results)
	out := map[string]any{
		"results": results,
		"summary": map[string]any{
			"total":  s.Total,
			"passed": s.Passed,
			"failed": s.Failed,
		},
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
```

`internal/report/junit.go`:

```go
package report

import (
	"encoding/xml"
	"fmt"
	"io"

	"github.com/yourorg/bt/pkg/model"
)

// JUnitTestSuites is the root XML element.
type JUnitTestSuites struct {
	XMLName xml.Name         `xml:"testsuites"`
	Suites  []JUnitTestSuite `xml:"testsuite"`
}

// JUnitTestSuite represents a single test suite.
type JUnitTestSuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Cases    []JUnitTestCase `xml:"testcase"`
}

// JUnitTestCase represents a single test case.
type JUnitTestCase struct {
	XMLName   xml.Name       `xml:"testcase"`
	Name      string         `xml:"name,attr"`
	Classname string         `xml:"classname,attr"`
	Failure   *JUnitFailure  `xml:"failure,omitempty"`
}

// JUnitFailure represents a test failure.
type JUnitFailure struct {
	Message string `xml:"message,attr"`
	Text    string `xml:",chardata"`
}

type junitReporter struct{ w io.Writer }

// NewJUnit returns a Reporter that writes JUnit-compatible XML.
func NewJUnit(w io.Writer) Reporter { return &junitReporter{w: w} }

func (r *junitReporter) Write(results []model.Result) error {
	s := summarise(results)
	suite := JUnitTestSuite{
		Name:     "bt",
		Tests:    s.Total,
		Failures: s.Failed,
	}

	for _, res := range results {
		tc := JUnitTestCase{
			Name:      res.CaseID,
			Classname: "bt",
		}
		if !res.Passed && len(res.Failures) > 0 {
			msgs := ""
			for _, f := range res.Failures {
				msgs += fmt.Sprintf("%s: %s\n", f.Invariant, f.Message)
			}
			tc.Failure = &JUnitFailure{
				Message: res.Failures[0].Message,
				Text:    msgs,
			}
		}
		suite.Cases = append(suite.Cases, tc)
	}

	suites := JUnitTestSuites{Suites: []JUnitTestSuite{suite}}
	out, err := xml.MarshalIndent(suites, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot marshal JUnit XML: %w", err)
	}
	_, err = r.w.Write(out)
	return err
}
```

Run the tests:

```bash
go test ./internal/report/... -race -v
```

---

## Step 6 — Wire bt run

### Spec

- `bt run` loads the config, discovers operations via the adapter, plans cases via the strategy, executes them, and writes a report
- `--strategy` flag selects the strategy (default: `table`)
- `--output` flag selects the report format (default: `console`)
- Exit code 0 if all cases pass, 1 if any case fails, 2 on config or execution error

### Tests

`internal/cli/run_test.go`:

```go
package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourorg/bt/internal/cli"
)

func TestRunCommand_TableStrategy_AllPass(t *testing.T) {
	// Stand up a test server that always returns 200.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()

	// Write a minimal OpenAPI spec.
	spec := `
openapi: "3.0.3"
info:
  title: Test API
  version: "1.0.0"
paths:
  /health:
    get:
      operationId: GetHealth
      responses:
        "200":
          description: OK
`
	specPath := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatal(err)
	}

	// Write a table case file.
	cases := `
cases:
  - id: health-check
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
`
	casesPath := filepath.Join(dir, "cases.yaml")
	if err := os.WriteFile(casesPath, []byte(cases), 0644); err != nil {
		t.Fatal(err)
	}

	// Write the config pointing at the test server.
	cfg := "version: 1\ntarget:\n  name: test-api\n  base_url: " + server.URL +
		"\n  schema: " + specPath + "\nstrategies:\n  - type: table\n    file: " + casesPath + "\n"
	cfgPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"run", "--config", cfgPath, "--strategy", "table"})
	cmd.SetOut(&bytes.Buffer{})

	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected run to succeed with all passing cases, got: %v", err)
	}
}

func TestRunCommand_TableStrategy_FailuresReturnError(t *testing.T) {
	// Server returns 500, but cases expect 200 — should fail.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()

	spec := `
openapi: "3.0.3"
info:
  title: Test API
  version: "1.0.0"
paths:
  /health:
    get:
      operationId: GetHealth
      responses:
        "200":
          description: OK
`
	specPath := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(specPath, []byte(spec), 0644); err != nil {
		t.Fatal(err)
	}

	cases := `
cases:
  - id: health-check
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
`
	casesPath := filepath.Join(dir, "cases.yaml")
	if err := os.WriteFile(casesPath, []byte(cases), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := "version: 1\ntarget:\n  name: test-api\n  base_url: " + server.URL +
		"\n  schema: " + specPath + "\nstrategies:\n  - type: table\n    file: " + casesPath + "\n"
	cfgPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"run", "--config", cfgPath, "--strategy", "table"})
	cmd.SetOut(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected run to return an error when cases fail")
	}
}
```

### Implementation

Update `internal/cli/run.go`:

```go
package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yourorg/bt/internal/adapter/openapi"
	"github.com/yourorg/bt/internal/config"
	"github.com/yourorg/bt/internal/report"
	"github.com/yourorg/bt/internal/runner"
	"github.com/yourorg/bt/internal/strategy"
	"github.com/yourorg/bt/internal/strategy/table"
	"time"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "run",
		Short:        "Run a test plan",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, _ := cmd.Flags().GetString("config")
			strategyName, _ := cmd.Flags().GetString("strategy")
			outputFormat, _ := cmd.Flags().GetString("output")

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			// Resolve adapter.
			adapter := openapi.New()
			ops, err := adapter.Discover(cmd.Context(), cfg.Target.AsModel())
			if err != nil {
				return fmt.Errorf("adapter: %w", err)
			}

			// Resolve strategy.
			var strat strategy.Strategy
			switch strategy.Kind(strategyName) {
			case strategy.KindTable, "":
				strat = table.New()
			default:
				return fmt.Errorf("unknown strategy: %q", strategyName)
			}

			// Find strategy config from config file.
			var spec strategy.Spec
			spec.Kind = strategy.Kind(strategyName)
			for _, sc := range cfg.Strategies {
				if sc.Type == strategyName || (strategyName == "" && sc.Type == "table") {
					spec.Config = sc.Config
					if spec.Config == nil {
						spec.Config = map[string]any{}
					}
					if sc.File != "" {
						spec.Config["file"] = sc.File
					}
					break
				}
			}

			cases, err := strat.Plan(cmd.Context(), spec, ops)
			if err != nil {
				return fmt.Errorf("plan: %w", err)
			}

			// Build executor.
			exec := runner.New(runner.Config{
				BaseURL: cfg.Target.BaseURL,
				Timeout: 30 * time.Second,
			})

			results, err := strat.Execute(cmd.Context(), cases, exec)
			if err != nil {
				return fmt.Errorf("execute: %w", err)
			}

			// Write report.
			var rep report.Reporter
			switch outputFormat {
			case "json":
				rep = report.NewJSON(cmd.OutOrStdout())
			case "junit":
				rep = report.NewJUnit(cmd.OutOrStdout())
			default:
				rep = report.NewConsole(cmd.OutOrStdout())
			}

			if err := rep.Write(results); err != nil {
				return fmt.Errorf("report: %w", err)
			}

			// Exit 1 if any case failed.
			for _, r := range results {
				if !r.Passed {
					return errors.New("one or more test cases failed")
				}
			}

			return nil
		},
	}

	cmd.Flags().String("strategy", "table", "strategy to run (table, property, fuzz, contract)")
	return cmd
}
```

Add an `AsModel()` helper to `TargetConfig` so the CLI can convert config to the model type:

`internal/config/loader.go` — add this method:

```go
func (t TargetConfig) AsModel() model.Target {
	return model.Target{
		Name:        t.Name,
		BaseURL:     t.BaseURL,
		SchemaPath:  t.SchemaPath,
		Environment: t.Environment,
		Auth: model.AuthConfig{
			Type: t.Auth.Type,
			Env:  t.Auth.Env,
		},
	}
}
```

Run the tests:

```bash
go test ./internal/cli/... -race -v
```

---

## Step 7 — Full verification

```bash
# All tests
go test ./... -race -v

# Lint
golangci-lint run ./...

# Build
CGO_ENABLED=0 go build ./cmd/bt

# End-to-end smoke test against a real API
# (replace with your own target)
./bt run --config backendtest.yaml --strategy table
./bt run --config backendtest.yaml --strategy table --output json
./bt run --config backendtest.yaml --strategy table --output junit
```

---

## M2 exit criterion

`bt run --strategy table` executes a YAML table of test cases against a real API, reports pass/fail per case to the console, exits 0 when all pass and 1 when any fail. JSON and JUnit outputs work. All code has tests written before implementation.