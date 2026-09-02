package table_test

import (
	"encoding/json"
	"testing"

	"github.com/jimbery/bt/internal/strategy/table"
	"github.com/jimbery/bt/pkg/model"
)

func buildSchemaRef(t *testing.T, raw string) *model.SchemaRef {
	t.Helper()
	var s model.SchemaRef
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("buildSchemaRef: %v", err)
	}
	return &s
}

var orderSchemaJSON = `{
  "type": "object",
  "required": ["id", "amount", "currency", "status"],
  "properties": {
    "id":       { "type": "string" },
    "amount":   { "type": "integer" },
    "currency": { "type": "string", "enum": ["GBP", "USD", "EUR"] },
    "status":   { "type": "string", "enum": ["pending", "confirmed", "cancelled"] },
    "note":     { "type": "string", "nullable": true }
  },
  "additionalProperties": false
}`

func TestSchemaAssertion_ValidBody_NoViolations(t *testing.T) {
	body := []byte(`{"id":"ord_1","amount":100,"currency":"GBP","status":"pending"}`)
	sa := table.NewSchemaAssertion(buildSchemaRef(t, orderSchemaJSON))
	violations := sa.Evaluate(body)
	if len(violations) != 0 {
		t.Errorf("expected no violations, got %d: %v", len(violations), violations)
	}
}

func TestSchemaAssertion_MissingRequiredField(t *testing.T) {
	body := []byte(`{"id":"ord_1","amount":100,"currency":"GBP"}`)
	sa := table.NewSchemaAssertion(buildSchemaRef(t, orderSchemaJSON))
	violations := sa.Evaluate(body)
	if len(violations) == 0 {
		t.Fatal("expected violation for missing required field 'status'")
	}
	assertViolationField(t, violations, "$.status")
	assertViolationSeverity(t, violations, "$.status", model.SeverityCritical)
}

func TestSchemaAssertion_WrongType(t *testing.T) {
	body := []byte(`{"id":"ord_1","amount":"not-a-number","currency":"GBP","status":"pending"}`)
	sa := table.NewSchemaAssertion(buildSchemaRef(t, orderSchemaJSON))
	violations := sa.Evaluate(body)
	if len(violations) == 0 {
		t.Fatal("expected violation for wrong type on field 'amount'")
	}
	v := findViolation(t, violations, "$.amount")
	if v.ExpectedType != "integer" {
		t.Errorf("ExpectedType: want integer, got %q", v.ExpectedType)
	}
	if v.ActualType != "string" {
		t.Errorf("ActualType: want string, got %q", v.ActualType)
	}
	if v.Severity != model.SeverityCritical {
		t.Errorf("severity: want Critical, got %v", v.Severity)
	}
}

func TestSchemaAssertion_EnumViolation(t *testing.T) {
	body := []byte(`{"id":"ord_1","amount":100,"currency":"INVALID","status":"pending"}`)
	sa := table.NewSchemaAssertion(buildSchemaRef(t, orderSchemaJSON))
	violations := sa.Evaluate(body)
	if len(violations) == 0 {
		t.Fatal("expected enum violation")
	}
	v := findViolation(t, violations, "$.currency")
	if v.Severity != model.SeverityCritical {
		t.Errorf("severity: want Critical, got %v", v.Severity)
	}
	if v.Message == "" {
		t.Error("Message must not be empty")
	}
}

func TestSchemaAssertion_NullableFieldAcceptsNull(t *testing.T) {
	body := []byte(`{"id":"ord_1","amount":100,"currency":"GBP","status":"pending","note":null}`)
	sa := table.NewSchemaAssertion(buildSchemaRef(t, orderSchemaJSON))
	if len(sa.Evaluate(body)) != 0 {
		t.Fatal("expected no violations for nullable null")
	}
}

func TestSchemaAssertion_NonNullableFieldIsNull(t *testing.T) {
	body := []byte(`{"id":"ord_1","amount":null,"currency":"GBP","status":"pending"}`)
	sa := table.NewSchemaAssertion(buildSchemaRef(t, orderSchemaJSON))
	violations := sa.Evaluate(body)
	if len(violations) == 0 {
		t.Fatal("expected violation for null on amount")
	}
	v := findViolation(t, violations, "$.amount")
	if v.Severity != model.SeverityCritical {
		t.Errorf("severity: want Critical, got %v", v.Severity)
	}
}

func TestSchemaAssertion_AdditionalPropertyForbidden(t *testing.T) {
	body := []byte(`{"id":"ord_1","amount":100,"currency":"GBP","status":"pending","unexpected_field":"oops"}`)
	sa := table.NewSchemaAssertion(buildSchemaRef(t, orderSchemaJSON))
	violations := sa.Evaluate(body)
	if len(violations) == 0 {
		t.Fatal("expected violation for additional property")
	}
	v := findViolation(t, violations, "$.unexpected_field")
	if v.Severity != model.SeverityWarning {
		t.Errorf("severity: want Warning, got %v", v.Severity)
	}
}

func TestSchemaAssertion_NestedObjectViolation_FullPath(t *testing.T) {
	schema := buildSchemaRef(t, `{
		"type": "object",
		"required": ["id", "address"],
		"properties": {
			"id": { "type": "string" },
			"address": {
				"type": "object",
				"required": ["postcode"],
				"properties": {
					"postcode": { "type": "string" },
					"line1":    { "type": "string" }
				}
			}
		}
	}`)
	body := []byte(`{"id":"ord_1","address":{"line1":"123 Street"}}`)
	violations := table.NewSchemaAssertion(schema).Evaluate(body)
	if len(violations) == 0 {
		t.Fatal("expected nested violation")
	}
	assertViolationField(t, violations, "$.address.postcode")
}

func TestSchemaAssertion_ArrayItemViolation_IndexedPath(t *testing.T) {
	schema := buildSchemaRef(t, `{
		"type": "object",
		"properties": {
			"items": {
				"type": "array",
				"items": {
					"type": "object",
					"required": ["sku"],
					"properties": {
						"sku": { "type": "string" },
						"qty": { "type": "integer" }
					}
				}
			}
		}
	}`)
	body := []byte(`{"items":[{"sku":"A1","qty":2},{"qty":1}]}`)
	violations := table.NewSchemaAssertion(schema).Evaluate(body)
	if len(violations) == 0 {
		t.Fatal("expected violation for items[1].sku")
	}
	assertViolationField(t, violations, "$.items[1].sku")
}

func TestSchemaAssertion_EmptyBodyWithObjectSchema(t *testing.T) {
	schema := buildSchemaRef(t, `{"type":"object","required":["id"],"properties":{"id":{"type":"string"}}}`)
	violations := table.NewSchemaAssertion(schema).Evaluate([]byte{})
	if len(violations) == 0 {
		t.Fatal("expected violation for empty body")
	}
	assertViolationField(t, violations, "$")
	assertViolationSeverity(t, violations, "$", model.SeverityCritical)
}

func TestSchemaAssertion_NonJSONBody(t *testing.T) {
	schema := buildSchemaRef(t, `{"type":"object"}`)
	violations := table.NewSchemaAssertion(schema).Evaluate([]byte("not valid json"))
	if len(violations) == 0 {
		t.Fatal("expected violation for non-JSON")
	}
	v := findViolation(t, violations, "$")
	if v.Severity != model.SeverityCritical {
		t.Error("want Critical for non-JSON")
	}
	if v.Message == "" || v.Message == "schema mismatch" {
		t.Errorf("message: %q", v.Message)
	}
}

func TestSchemaAssertion_MultipleViolations_AllReported(t *testing.T) {
	body := []byte(`{"id":"ord_1","amount":"bad","status":"unknown"}`)
	sa := table.NewSchemaAssertion(buildSchemaRef(t, orderSchemaJSON))
	violations := sa.Evaluate(body)
	if len(violations) < 3 {
		t.Fatalf("expected at least 3 violations, got %d: %v", len(violations), violations)
	}
}

func findViolation(t *testing.T, violations []model.SchemaViolation, field string) model.SchemaViolation {
	t.Helper()
	for _, v := range violations {
		if v.Field == field {
			return v
		}
	}
	t.Fatalf("no violation for field %q: %v", field, violations)
	return model.SchemaViolation{}
}

func assertViolationField(t *testing.T, violations []model.SchemaViolation, field string) {
	t.Helper()
	_ = findViolation(t, violations, field)
}

func assertViolationSeverity(t *testing.T, violations []model.SchemaViolation, field string, want model.ViolationSeverity) {
	t.Helper()
	v := findViolation(t, violations, field)
	if v.Severity != want {
		t.Errorf("field %q: severity want %v got %v", field, want, v.Severity)
	}
}
