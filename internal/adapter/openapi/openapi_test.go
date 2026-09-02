package openapi_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jimbery/bt/internal/adapter/openapi"
	"github.com/jimbery/bt/pkg/model"
)

func writeSpec(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
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
