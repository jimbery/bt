package contract_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jayimbery/bt/internal/strategy/contract"
	"github.com/jayimbery/bt/pkg/model"
)

func schemaRef(t *testing.T, raw string) *model.SchemaRef {
	t.Helper()
	var s model.SchemaRef
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("schemaRef: %v", err)
	}
	return &s
}

func bodyOf(t *testing.T, raw string) map[string]any {
	t.Helper()
	var b map[string]any
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatalf("bodyOf: %v", err)
	}
	return b
}

func TestContractAssertion_RequiredFieldPresent_NoViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"required": ["id", "status"],
		"properties": {
			"id":     {"type": "string"},
			"status": {"type": "string"}
		}
	}`)
	body := bodyOf(t, `{"id": "ord_1", "status": "pending"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 0 {
		t.Errorf("expected no violations, got %d: %v", len(violations), violations)
	}
}

func TestContractAssertion_RequiredFieldMissing_CriticalViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"required": ["id", "status"],
		"properties": {
			"id":     {"type": "string"},
			"status": {"type": "string"}
		}
	}`)
	body := bodyOf(t, `{"id": "ord_1"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Field != "status" {
		t.Errorf("expected violation on field 'status', got %q", violations[0].Field)
	}
	if violations[0].Severity != contract.Critical {
		t.Errorf("expected Critical severity, got %v", violations[0].Severity)
	}
}

func TestContractAssertion_MultipleRequiredFieldsMissing_OneCriticalViolationEach(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"required": ["id", "status", "amount"],
		"properties": {
			"id":     {"type": "string"},
			"status": {"type": "string"},
			"amount": {"type": "integer"}
		}
	}`)
	body := bodyOf(t, `{}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 3 {
		t.Fatalf("expected 3 violations, got %d: %v", len(violations), violations)
	}
	for _, v := range violations {
		if v.Severity != contract.Critical {
			t.Errorf("expected Critical severity for %q, got %v", v.Field, v.Severity)
		}
	}
}

func TestContractAssertion_StringFieldWithIntegerValue_CriticalViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		}
	}`)
	body := bodyOf(t, `{"name": 42}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Field != "name" {
		t.Errorf("expected violation on 'name', got %q", violations[0].Field)
	}
	if violations[0].Severity != contract.Critical {
		t.Errorf("expected Critical severity, got %v", violations[0].Severity)
	}
	if violations[0].Expected == "" || violations[0].Actual == "" {
		t.Errorf("expected non-empty Expected and Actual, got %#v", violations[0])
	}
}

func TestContractAssertion_IntegerFieldWithStringValue_CriticalViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"amount": {"type": "integer"}
		}
	}`)
	body := bodyOf(t, `{"amount": "not-a-number"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Severity != contract.Critical {
		t.Errorf("expected Critical, got %v", violations[0].Severity)
	}
}

func TestContractAssertion_BooleanFieldWithStringValue_CriticalViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"active": {"type": "boolean"}
		}
	}`)
	body := bodyOf(t, `{"active": "true"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Field != "active" {
		t.Errorf("expected violation on 'active', got %q", violations[0].Field)
	}
}

func TestContractAssertion_NullableFieldIsNull_NoViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"description": {"type": "string", "nullable": true}
		}
	}`)
	body := bodyOf(t, `{"description": null}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 0 {
		t.Errorf("expected no violations for nullable null, got %d: %v", len(violations), violations)
	}
}

func TestContractAssertion_NonNullableFieldIsNull_CriticalViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"required": ["id"],
		"properties": {
			"id": {"type": "string", "nullable": false}
		}
	}`)
	body := bodyOf(t, `{"id": null}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Field != "id" {
		t.Errorf("expected violation on 'id', got %q", violations[0].Field)
	}
	if violations[0].Severity != contract.Critical {
		t.Errorf("expected Critical, got %v", violations[0].Severity)
	}
}

func TestContractAssertion_EnumFieldWithValidValue_NoViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"status": {"type": "string", "enum": ["pending", "confirmed", "cancelled"]}
		}
	}`)
	body := bodyOf(t, `{"status": "confirmed"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 0 {
		t.Errorf("expected no violations, got %d: %v", len(violations), violations)
	}
}

func TestContractAssertion_EnumFieldWithInvalidValue_CriticalViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"status": {"type": "string", "enum": ["pending", "confirmed", "cancelled"]}
		}
	}`)
	body := bodyOf(t, `{"status": "unknown_status"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Field != "status" {
		t.Errorf("expected violation on 'status', got %q", violations[0].Field)
	}
	if violations[0].Severity != contract.Critical {
		t.Errorf("expected Critical, got %v", violations[0].Severity)
	}
	if !strings.Contains(violations[0].Actual, "unknown_status") {
		t.Errorf("expected Actual to mention unknown_status, got %q", violations[0].Actual)
	}
}

func TestContractAssertion_AdditionalPropertiesFalse_UndeclaredFieldIsWarning(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"id": {"type": "string"}
		}
	}`)
	body := bodyOf(t, `{"id": "ord_1", "undeclared_field": "surprise"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Severity != contract.Warning {
		t.Errorf("expected Warning severity for extra field, got %v", violations[0].Severity)
	}
}

func TestContractAssertion_AdditionalPropertiesNotSet_UndeclaredFieldIgnored(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"id": {"type": "string"}
		}
	}`)
	body := bodyOf(t, `{"id": "ord_1", "extra": "value"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 0 {
		t.Errorf("expected no violations when additionalProperties not set, got %d: %v", len(violations), violations)
	}
}

func TestContractAssertion_NestedObjectRequiredFieldMissing_ReportsFullPath(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"address": {
				"type": "object",
				"required": ["postcode"],
				"properties": {
					"postcode": {"type": "string"}
				}
			}
		}
	}`)
	body := bodyOf(t, `{"address": {}}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %v", len(violations), violations)
	}
	if violations[0].Field != "address.postcode" {
		t.Errorf("expected field path 'address.postcode', got %q", violations[0].Field)
	}
}

func TestContractAssertion_ArrayItemsWrongType_CriticalViolation(t *testing.T) {
	schema := schemaRef(t, `{
		"type": "object",
		"properties": {
			"tags": {
				"type": "array",
				"items": {"type": "string"}
			}
		}
	}`)
	body := bodyOf(t, `{"tags": ["valid", 42, "also-valid"]}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 1 {
		t.Fatalf("expected 1 violation for invalid array item, got %d: %v", len(violations), violations)
	}
	if violations[0].Field != "tags[1]" {
		t.Errorf("expected field path 'tags[1]', got %q", violations[0].Field)
	}
}
