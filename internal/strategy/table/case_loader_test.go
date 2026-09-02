package table_test

import (
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/strategy/table"
)

const caseWithInlineSchema = `
cases:
  - id: get-order-schema
    operation_id: GetOrder
    input:
      method: GET
      path: /orders/ord_1
    expected:
      status_code: 200
      schema:
        type: object
        required: [id, amount, status]
        properties:
          id:     { type: string }
          amount: { type: integer }
          status: { type: string }
`

const caseWithRefSchema = `
cases:
  - id: get-order-ref
    operation_id: GetOrder
    input:
      method: GET
      path: /orders/ord_1
    expected:
      status_code: 200
      schema:
        $ref: "#/components/schemas/Order"
`

const caseWithGQLDataSchema = `
cases:
  - id: gql-create-order
    operation_id: createOrder
    input:
      method: POST
      path: /graphql
      gql_query: "mutation { createOrder(amount: 100, currency: GBP) { id status } }"
    expected:
      status_code: 200
      gql_data_schema:
        type: object
        properties:
          createOrder:
            type: object
            required: [id, status]
            properties:
              id:     { type: string }
              status: { type: string }
`

const caseWithoutSchema = `
cases:
  - id: health-check
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
`

func minimalOrdersOpenAPI(t *testing.T) []byte {
	t.Helper()
	return []byte(`openapi: 3.0.3
info:
  title: Orders
  version: "1"
paths: {}
components:
  schemas:
    Order:
      type: object
      required: [id]
      properties:
        id:
          type: string
`)
}

func TestCaseLoader_InlineSchema_ParsedCorrectly(t *testing.T) {
	cases, err := table.LoadCasesFromReader(strings.NewReader(caseWithInlineSchema))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("cases: %d", len(cases))
	}
	c := cases[0]
	if c.Expected == nil || c.Expected.Schema == nil {
		t.Fatal("expected Schema")
	}
	if c.Expected.Schema.Type != "object" {
		t.Errorf("type=%q", c.Expected.Schema.Type)
	}
	if len(c.Expected.Schema.Required) == 0 {
		t.Error("expected required fields")
	}
}

func TestCaseLoader_RefSchema_ParsedAsRef(t *testing.T) {
	cases, err := table.LoadCasesFromReader(strings.NewReader(caseWithRefSchema))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c := cases[0]
	if c.Expected.Schema == nil {
		t.Fatal("expected Schema")
	}
	if c.Expected.Schema.Ref != "#/components/schemas/Order" {
		t.Errorf("Ref=%q", c.Expected.Schema.Ref)
	}
}

func TestCaseLoader_GQLDataSchema_ParsedCorrectly(t *testing.T) {
	cases, err := table.LoadCasesFromReader(strings.NewReader(caseWithGQLDataSchema))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c := cases[0]
	if c.Expected.GQLDataSchema == nil {
		t.Fatal("expected GQLDataSchema")
	}
	opSchema, ok := c.Expected.GQLDataSchema.Properties["createOrder"]
	if !ok || opSchema == nil {
		t.Fatal("expected GQLDataSchema property createOrder")
	}
	if opSchema.Type != "object" {
		t.Errorf("createOrder type=%q", opSchema.Type)
	}
}

func TestCaseLoader_NoSchema_SchemaIsNil(t *testing.T) {
	cases, err := table.LoadCasesFromReader(strings.NewReader(caseWithoutSchema))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	c := cases[0]
	if c.Expected.Schema != nil {
		t.Error("Schema should be nil")
	}
	if c.Expected.GQLDataSchema != nil {
		t.Error("GQLDataSchema should be nil")
	}
}

func TestCaseLoader_InvalidRefSchema_ReturnsConfigError(t *testing.T) {
	yaml := `
cases:
  - id: bad-ref
    operation_id: GetOrder
    input:
      method: GET
      path: /orders/ord_1
    expected:
      status_code: 200
      schema:
        $ref: "#/components/schemas/DoesNotExist"
`
	_, err := table.LoadCasesWithSpec(strings.NewReader(yaml), minimalOrdersOpenAPI(t))
	if err == nil {
		t.Fatal("expected error")
	}
	if !table.IsConfigError(err) {
		t.Errorf("want ConfigError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "DoesNotExist") {
		t.Errorf("error=%v", err)
	}
}

func TestCaseLoader_InvalidYAML_ReturnsError(t *testing.T) {
	_, err := table.LoadCasesFromReader(strings.NewReader("not: valid: yaml: :::"))
	if err == nil {
		t.Fatal("expected error")
	}
}
