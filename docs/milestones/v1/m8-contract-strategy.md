# M8 — Contract Strategy + CI Hardening

This document follows the same structure as M1–M7: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist. Tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

Response object and schema validation is required at every layer. Asserting only a status code is not sufficient — every test must verify the shape, field types, and semantics of the response body against the declared schema.

---

## Overview

M8 delivers contract verification as a first-class strategy mode, plus the CI hardening needed for `bt` to act as a real quality gate in a production pipeline.

The five pieces built here are:

1. **Contract strategy** — verifies that provider behaviour matches the OpenAPI schema contract, field by field, including nullable handling, enum constraints, and required/optional presence
2. **Baseline and quarantine model** — distinguishes known-failing cases from regressions so CI does not produce noise on expected failures
3. **Exit code model** — structured, machine-readable exit codes that CI systems can key on: pass, test failures, config error, execution error
4. **`bt doctor` command** — diagnoses environment and config problems before a run starts
5. **GitHub Actions install snippet and example workflow** — a ready-to-use CI integration that gates on failures and uploads structured results

Each piece has its own spec, tests, and implementation section. Build and verify each step before moving to the next.

**Exit criterion:** `bt run --strategy contract` runs in a real CI pipeline against the orders API, gates on genuine failures, does not fire on quarantined cases, and produces a structured exit code that the CI system can consume. `bt doctor` catches the five most common misconfiguration patterns before the run starts.

---

## Step 1 — Contract strategy

### Spec

The contract strategy lives at `internal/strategy/contract/`. It verifies that a live provider's responses conform to the OpenAPI schema contract — not just that the status code is correct, but that every field in the response body matches its declared type, format, nullability, enum membership, and required/optional presence.

- `ContractStrategy` implements `Strategy` (the interface from `internal/strategy/`)
- `Run(ctx context.Context, plan TestPlan) ([]Result, error)` — iterates operations, sends a minimal valid request for each, and evaluates the response against the schema
- For each operation, the contract runner:
  - Constructs a minimal valid request from the operation's schema (uses the same generator as the property strategy, but takes only the first generated value — no iteration)
  - Sends the request via the HTTP runner
  - Evaluates the response body against `ContractAssertion` (defined below)
  - Records a `ContractResult` per operation
- `ContractAssertion` evaluates a response against a schema. It checks:
  - Status code is in the operation's declared `responses` map
  - `Content-Type` header is `application/json` (or matches the declared media type)
  - Response body is valid JSON
  - Every required field is present
  - No field has a value of the wrong type (e.g. string where integer is declared)
  - Enum fields contain only declared values
  - Nullable fields may be `null`; non-nullable fields must not be `null`
  - No undeclared fields are present when `additionalProperties: false` is set
- `ContractViolation` represents a single schema disagreement:
  ```go
  type ContractViolation struct {
      Field       string // JSON path, e.g. "order.status"
      Expected    string // human description of the expected type/value
      Actual      string // what was found
      Severity    ViolationSeverity // Critical | Warning
  }
  ```
- `ViolationSeverity`:
  - `Critical` — missing required field, wrong type, null where non-nullable; these cause `Result.Passed = false`
  - `Warning` — extra undeclared field when `additionalProperties` is unset (permitted by spec); these are recorded but do not fail the result
- `ContractResult` extends `Result` with:
  ```go
  type ContractResult struct {
      Result
      Violations []ContractViolation
      SchemaPath string // the OpenAPI schema ref that was evaluated
  }
  ```
- The strategy honours context cancellation — if `ctx` is cancelled mid-run, the current operation finishes and the run stops cleanly
- The strategy does not mutate shared state — it is safe to run in parallel with `-race`

### Tests

`internal/strategy/contract/assertion_test.go`:

```go
package contract_test

import (
	"encoding/json"
	"testing"

	"github.com/jimbery/bt/internal/strategy/contract"
	"github.com/jimbery/bt/pkg/model"
)

// schemaFor is a helper that returns a model.Schema with the given fields pre-set.
func schemaFor(t *testing.T, raw string) model.Schema {
	t.Helper()
	var s model.Schema
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatalf("schemaFor: invalid JSON: %v", err)
	}
	return s
}

// bodyOf is a helper that returns a parsed JSON body.
func bodyOf(t *testing.T, raw string) map[string]any {
	t.Helper()
	var b map[string]any
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatalf("bodyOf: invalid JSON: %v", err)
	}
	return b
}

// --- Required field presence ---

func TestContractAssertion_RequiredFieldPresent_NoViolation(t *testing.T) {
	schema := schemaFor(t, `{
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
	schema := schemaFor(t, `{
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
	schema := schemaFor(t, `{
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

// --- Type checking ---

func TestContractAssertion_StringFieldWithStringValue_NoViolation(t *testing.T) {
	schema := schemaFor(t, `{
		"type": "object",
		"properties": {
			"name": {"type": "string"}
		}
	}`)
	body := bodyOf(t, `{"name": "test"}`)

	violations := contract.EvaluateBody(body, schema)

	if len(violations) != 0 {
		t.Errorf("expected no violations, got %d: %v", len(violations), violations)
	}
}

func TestContractAssertion_StringFieldWithIntegerValue_CriticalViolation(t *testing.T) {
	schema := schemaFor(t, `{
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
	// The violation must describe what was expected and what was found.
	if violations[0].Expected == "" {
		t.Error("Expected field must be non-empty")
	}
	if violations[0].Actual == "" {
		t.Error("Actual field must be non-empty")
	}
}

func TestContractAssertion_IntegerFieldWithStringValue_CriticalViolation(t *testing.T) {
	schema := schemaFor(t, `{
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
	schema := schemaFor(t, `{
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

// --- Nullable handling ---

func TestContractAssertion_NullableFieldIsNull_NoViolation(t *testing.T) {
	schema := schemaFor(t, `{
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
	schema := schemaFor(t, `{
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

// --- Enum checking ---

func TestContractAssertion_EnumFieldWithValidValue_NoViolation(t *testing.T) {
	schema := schemaFor(t, `{
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
	schema := schemaFor(t, `{
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
	// Actual value must be reported verbatim.
	if violations[0].Actual != "unknown_status" {
		t.Errorf("expected Actual to be 'unknown_status', got %q", violations[0].Actual)
	}
}

// --- Additional properties ---

func TestContractAssertion_AdditionalPropertiesFalse_UndeclaredFieldIsWarning(t *testing.T) {
	schema := schemaFor(t, `{
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
	schema := schemaFor(t, `{
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

// --- Nested objects ---

func TestContractAssertion_NestedObjectRequiredFieldMissing_ReportsFullPath(t *testing.T) {
	schema := schemaFor(t, `{
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
	// Field path must include the parent: "address.postcode"
	if violations[0].Field != "address.postcode" {
		t.Errorf("expected field path 'address.postcode', got %q", violations[0].Field)
	}
}

// --- Array items ---

func TestContractAssertion_ArrayItemsWrongType_CriticalViolation(t *testing.T) {
	schema := schemaFor(t, `{
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
	// Field path must include array index: "tags[1]"
	if violations[0].Field != "tags[1]" {
		t.Errorf("expected field path 'tags[1]', got %q", violations[0].Field)
	}
}
```

`internal/strategy/contract/strategy_test.go`:

```go
package contract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jimbery/bt/internal/strategy/contract"
	"github.com/jimbery/bt/pkg/model"
)

// newContractServer builds a test HTTP server with configurable per-path handlers.
func newContractServer(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	return httptest.NewServer(mux)
}

// writeJSON is a test helper that writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// orderSchema returns the canonical order schema used in contract tests.
func orderSchema() model.Schema {
	raw := `{
		"type": "object",
		"required": ["id", "amount", "currency", "status", "created_at"],
		"properties": {
			"id":         {"type": "string"},
			"amount":     {"type": "integer"},
			"currency":   {"type": "string"},
			"description":{"type": "string", "nullable": true},
			"status":     {"type": "string", "enum": ["pending", "confirmed", "shipped", "delivered", "cancelled"]},
			"created_at": {"type": "string", "format": "date-time"}
		}
	}`
	var s model.Schema
	_ = json.Unmarshal([]byte(raw), &s)
	return s
}

// --- Happy path ---

func TestContractStrategy_OperationReturnsConformingResponse_Passes(t *testing.T) {
	srv := newContractServer(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"id":         "ord_1",
				"amount":     100,
				"currency":   "GBP",
				"description": nil,
				"status":     "pending",
				"created_at": "2024-01-01T00:00:00Z",
			})
		},
	})
	defer srv.Close()

	plan := model.TestPlan{
		Target: model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{
			{
				ID:     "GetOrder",
				Method: "GET",
				Path:   "/orders/probe",
				ResponseSchema: map[int]model.Schema{
					200: orderSchema(),
				},
			},
		},
	}

	results, err := contract.NewStrategy().Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	r := results[0].(contract.ContractResult)

	if !r.Passed {
		t.Errorf("expected result to pass, got violations: %v", r.Violations)
	}
	if len(r.Violations) != 0 {
		t.Errorf("expected no violations, got %d: %v", len(r.Violations), r.Violations)
	}
	if r.SchemaPath == "" {
		t.Error("SchemaPath must be non-empty")
	}
}

// --- Schema violation causes failure ---

func TestContractStrategy_ResponseMissingRequiredField_FailsWithCriticalViolation(t *testing.T) {
	srv := newContractServer(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			// Missing "status" and "created_at" required fields.
			writeJSON(w, http.StatusOK, map[string]any{
				"id":       "ord_1",
				"amount":   100,
				"currency": "GBP",
			})
		},
	})
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/probe", ResponseSchema: map[int]model.Schema{200: orderSchema()}}},
	}

	results, err := contract.NewStrategy().Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0].(contract.ContractResult)

	if r.Passed {
		t.Error("expected result to fail when required fields are missing")
	}

	criticals := 0
	for _, v := range r.Violations {
		if v.Severity == contract.Critical {
			criticals++
		}
	}
	if criticals < 2 {
		t.Errorf("expected at least 2 Critical violations (status, created_at), got %d", criticals)
	}
}

func TestContractStrategy_ResponseContainsWrongType_FailsWithCriticalViolation(t *testing.T) {
	srv := newContractServer(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			// amount is declared as integer; returning a string is a contract violation.
			writeJSON(w, http.StatusOK, map[string]any{
				"id":         "ord_1",
				"amount":     "one hundred",
				"currency":   "GBP",
				"status":     "pending",
				"created_at": "2024-01-01T00:00:00Z",
			})
		},
	})
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/probe", ResponseSchema: map[int]model.Schema{200: orderSchema()}}},
	}

	results, err := contract.NewStrategy().Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0].(contract.ContractResult)

	if r.Passed {
		t.Error("expected result to fail when field type is wrong")
	}

	found := false
	for _, v := range r.Violations {
		if v.Field == "amount" && v.Severity == contract.Critical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Critical violation on 'amount', violations: %v", r.Violations)
	}
}

func TestContractStrategy_ResponseContainsInvalidEnumValue_FailsWithCriticalViolation(t *testing.T) {
	srv := newContractServer(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"id":         "ord_1",
				"amount":     100,
				"currency":   "GBP",
				"status":     "dispatched", // not in enum
				"created_at": "2024-01-01T00:00:00Z",
			})
		},
	})
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/probe", ResponseSchema: map[int]model.Schema{200: orderSchema()}}},
	}

	results, err := contract.NewStrategy().Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0].(contract.ContractResult)

	if r.Passed {
		t.Error("expected result to fail on invalid enum value")
	}
	found := false
	for _, v := range r.Violations {
		if v.Field == "status" {
			found = true
			if v.Actual != "dispatched" {
				t.Errorf("expected Actual to report the invalid value 'dispatched', got %q", v.Actual)
			}
		}
	}
	if !found {
		t.Errorf("expected violation on 'status', violations: %v", r.Violations)
	}
}

// --- Status code not in declared responses ---

func TestContractStrategy_UndeclaredStatusCode_FailsWithCriticalViolation(t *testing.T) {
	srv := newContractServer(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			// Server returns 202 but only 200 is declared.
			writeJSON(w, http.StatusAccepted, map[string]any{"id": "ord_1"})
		},
	})
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/probe", ResponseSchema: map[int]model.Schema{200: orderSchema()}}},
	}

	results, err := contract.NewStrategy().Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0].(contract.ContractResult)

	if r.Passed {
		t.Error("expected result to fail when status code is not declared in the schema")
	}
}

// --- Content-Type ---

func TestContractStrategy_MissingContentTypeHeader_FailsWithCriticalViolation(t *testing.T) {
	srv := newContractServer(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			// No Content-Type header set.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"ord_1","amount":100,"currency":"GBP","status":"pending","created_at":"2024-01-01T00:00:00Z"}`))
		},
	})
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/probe", ResponseSchema: map[int]model.Schema{200: orderSchema()}}},
	}

	results, err := contract.NewStrategy().Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r := results[0].(contract.ContractResult)

	if r.Passed {
		t.Error("expected result to fail when Content-Type is missing")
	}
}

// --- Context cancellation ---

func TestContractStrategy_ContextCancelledMidRun_ReturnsContextError(t *testing.T) {
	// Block forever so cancellation is the only way out.
	srv := newContractServer(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		},
	})
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/probe", ResponseSchema: map[int]model.Schema{200: orderSchema()}}},
	}

	_, err := contract.NewStrategy().Run(ctx, plan)
	if err == nil {
		t.Error("expected an error when context is cancelled, got nil")
	}
}

// --- Multiple operations ---

func TestContractStrategy_MultipleOperations_AllResultsReturned(t *testing.T) {
	srv := newContractServer(t, map[string]http.HandlerFunc{
		"/health": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		},
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"id": "ord_1", "amount": 100, "currency": "GBP",
				"status": "pending", "created_at": "2024-01-01T00:00:00Z",
			})
		},
	})
	defer srv.Close()

	healthSchema := model.Schema{} // minimal — just checks status code
	_ = json.Unmarshal([]byte(`{"type":"object","required":["status"],"properties":{"status":{"type":"string"}}}`), &healthSchema)

	plan := model.TestPlan{
		Target: model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{
			{ID: "GetHealth", Method: "GET", Path: "/health", ResponseSchema: map[int]model.Schema{200: healthSchema}},
			{ID: "GetOrder", Method: "GET", Path: "/orders/probe", ResponseSchema: map[int]model.Schema{200: orderSchema()}},
		},
	}

	results, err := contract.NewStrategy().Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		cr := r.(contract.ContractResult)
		if !cr.Passed {
			t.Errorf("result[%d] failed unexpectedly: %v", i, cr.Violations)
		}
	}
}
```

### Implementation

`internal/strategy/contract/violation.go`:

```go
package contract

// ViolationSeverity classifies a contract violation's impact.
type ViolationSeverity int

const (
	Critical ViolationSeverity = iota // causes Result.Passed = false
	Warning                           // recorded but does not fail the result
)

func (s ViolationSeverity) String() string {
	switch s {
	case Critical:
		return "critical"
	case Warning:
		return "warning"
	default:
		return "unknown"
	}
}

// ContractViolation is a single disagreement between a response and its schema.
type ContractViolation struct {
	Field    string            // JSON path, e.g. "order.status" or "tags[1]"
	Expected string            // human description of what the schema declares
	Actual   string            // what was found in the response
	Severity ViolationSeverity
}
```

`internal/strategy/contract/assertion.go`:

```go
package contract

import (
	"fmt"

	"github.com/jimbery/bt/pkg/model"
)

// EvaluateBody checks a decoded response body against a Schema and returns
// every violation found. It never returns nil — an empty slice means no violations.
func EvaluateBody(body map[string]any, schema model.Schema) []ContractViolation {
	var violations []ContractViolation
	evaluateObject(body, schema, "", &violations)
	return violations
}

func evaluateObject(body map[string]any, schema model.Schema, prefix string, out *[]ContractViolation) {
	// Required field presence.
	for _, req := range schema.Required {
		if _, ok := body[req]; !ok {
			*out = append(*out, ContractViolation{
				Field:    fieldPath(prefix, req),
				Expected: fmt.Sprintf("required field of type %s", schema.Properties[req].Type),
				Actual:   "field absent",
				Severity: Critical,
			})
		}
	}

	// Per-field type and constraint checks.
	for name, propSchema := range schema.Properties {
		val, present := body[name]
		if !present {
			continue // absence of optional fields is not a violation
		}

		path := fieldPath(prefix, name)

		// Null check.
		if val == nil {
			if !propSchema.Nullable {
				*out = append(*out, ContractViolation{
					Field:    path,
					Expected: fmt.Sprintf("non-nullable %s", propSchema.Type),
					Actual:   "null",
					Severity: Critical,
				})
			}
			continue
		}

		// Type check.
		if propSchema.Type != "" {
			if v := checkType(val, propSchema.Type, path); v != nil {
				*out = append(*out, *v)
				continue // type wrong; skip further checks on this field
			}
		}

		// Enum check.
		if len(propSchema.Enum) > 0 {
			if v := checkEnum(val, propSchema.Enum, path); v != nil {
				*out = append(*out, *v)
			}
		}

		// Nested object.
		if propSchema.Type == "object" {
			nested, ok := val.(map[string]any)
			if ok {
				evaluateObject(nested, propSchema, path, out)
			}
		}

		// Array items.
		if propSchema.Type == "array" && propSchema.Items != nil {
			arr, ok := val.([]any)
			if ok {
				for i, item := range arr {
					itemPath := fmt.Sprintf("%s[%d]", path, i)
					if item == nil && !propSchema.Items.Nullable {
						*out = append(*out, ContractViolation{
							Field:    itemPath,
							Expected: fmt.Sprintf("non-nullable array item of type %s", propSchema.Items.Type),
							Actual:   "null",
							Severity: Critical,
						})
						continue
					}
					if v := checkType(item, propSchema.Items.Type, itemPath); v != nil {
						*out = append(*out, *v)
					}
				}
			}
		}
	}

	// Additional properties check.
	if schema.AdditionalProperties != nil && !*schema.AdditionalProperties {
		for name := range body {
			if _, declared := schema.Properties[name]; !declared {
				*out = append(*out, ContractViolation{
					Field:    fieldPath(prefix, name),
					Expected: "no additional properties",
					Actual:   fmt.Sprintf("undeclared field %q present", name),
					Severity: Warning,
				})
			}
		}
	}
}

func checkType(val any, expected string, path string) *ContractViolation {
	ok := false
	switch expected {
	case "string":
		_, ok = val.(string)
	case "integer", "number":
		// JSON numbers decode as float64.
		f, isFloat := val.(float64)
		ok = isFloat && (expected == "number" || f == float64(int64(f)))
	case "boolean":
		_, ok = val.(bool)
	case "array":
		_, ok = val.([]any)
	case "object":
		_, ok = val.(map[string]any)
	default:
		ok = true // unknown type — do not flag
	}
	if !ok {
		return &ContractViolation{
			Field:    path,
			Expected: fmt.Sprintf("type %s", expected),
			Actual:   fmt.Sprintf("type %T with value %v", val, val),
			Severity: Critical,
		}
	}
	return nil
}

func checkEnum(val any, allowed []string, path string) *ContractViolation {
	s, ok := val.(string)
	if !ok {
		return nil // type mismatch already caught above
	}
	for _, a := range allowed {
		if s == a {
			return nil
		}
	}
	return &ContractViolation{
		Field:    path,
		Expected: fmt.Sprintf("one of %v", allowed),
		Actual:   s,
		Severity: Critical,
	}
}

func fieldPath(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}
```

`internal/strategy/contract/result.go`:

```go
package contract

import "github.com/jimbery/bt/pkg/model"

// ContractResult extends model.Result with contract-specific detail.
type ContractResult struct {
	model.Result
	Violations []ContractViolation
	SchemaPath string // the OpenAPI schema ref evaluated, e.g. "#/components/schemas/Order"
}
```

`internal/strategy/contract/strategy.go`:

```go
package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jimbery/bt/pkg/model"
)

// Strategy is the contract verification strategy.
type Strategy struct {
	client *http.Client
}

// NewStrategy returns a Strategy with sensible defaults.
func NewStrategy() *Strategy {
	return &Strategy{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Run executes the contract strategy against all operations in plan.
func (s *Strategy) Run(ctx context.Context, plan model.TestPlan) ([]model.Result, error) {
	var results []model.Result

	for _, op := range plan.Operations {
		select {
		case <-ctx.Done():
			return results, ctx.Err()
		default:
		}

		cr, err := s.runOperation(ctx, plan.Target, op)
		if err != nil {
			return results, fmt.Errorf("operation %s: %w", op.ID, err)
		}
		results = append(results, cr)
	}
	return results, nil
}

func (s *Strategy) runOperation(ctx context.Context, target model.Target, op model.Operation) (ContractResult, error) {
	url := target.BaseURL + op.Path
	req, err := http.NewRequestWithContext(ctx, op.Method, url, nil)
	if err != nil {
		return ContractResult{}, err
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return ContractResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ContractResult{}, err
	}

	var violations []ContractViolation

	// Content-Type check.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		violations = append(violations, ContractViolation{
			Field:    "Content-Type",
			Expected: "application/json",
			Actual:   ct,
			Severity: Critical,
		})
	}

	// Status code declared in schema.
	schema, statusDeclared := op.ResponseSchema[resp.StatusCode]
	if !statusDeclared {
		violations = append(violations, ContractViolation{
			Field:    "status_code",
			Expected: fmt.Sprintf("one of declared codes: %v", declaredCodes(op.ResponseSchema)),
			Actual:   fmt.Sprintf("%d", resp.StatusCode),
			Severity: Critical,
		})
		return ContractResult{
			Result:     model.Result{OperationID: op.ID, Passed: false},
			Violations: violations,
		}, nil
	}

	// Body parse.
	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		violations = append(violations, ContractViolation{
			Field:    "body",
			Expected: "valid JSON object",
			Actual:   fmt.Sprintf("parse error: %v", err),
			Severity: Critical,
		})
		return ContractResult{
			Result:     model.Result{OperationID: op.ID, Passed: false},
			Violations: violations,
		}, nil
	}

	violations = append(violations, EvaluateBody(decoded, schema)...)

	passed := true
	for _, v := range violations {
		if v.Severity == Critical {
			passed = false
			break
		}
	}

	return ContractResult{
		Result:     model.Result{OperationID: op.ID, Passed: passed},
		Violations: violations,
		SchemaPath: schema.Ref,
	}, nil
}

func declaredCodes(m map[int]model.Schema) []int {
	codes := make([]int, 0, len(m))
	for code := range m {
		codes = append(codes, code)
	}
	return codes
}
```

---

## Step 2 — Baseline and quarantine model

### Spec

The baseline model allows teams to mark known-failing contract cases as quarantined so they do not block CI while a fix is in progress. It does not hide bugs — it makes them visible but non-blocking.

- `Baseline` is a YAML file at `.bt/baseline.yaml` (or configurable path)
- Each entry declares an `operation_id` and a reason; optionally a `quarantine_until` date
- The contract runner checks each `ContractResult` against the baseline before determining the final exit code
- A quarantined failure: recorded in the report with `Quarantined: true`; does not contribute to the failure count
- A quarantine that has expired (`quarantine_until` is in the past): treated as a live failure; a warning is printed
- A baseline entry for an operation that passes: flagged as "stale baseline entry" — a warning, not an error
- Baseline file format:
  ```yaml
  version: 1
  quarantined:
    - operation_id: GetOrderBroken
      reason: "Known schema mismatch — tracked in ISSUE-42"
      quarantine_until: "2025-12-31"
  ```

### Tests

`internal/strategy/contract/baseline_test.go`:

```go
package contract_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jimbery/bt/internal/strategy/contract"
)

func writeBaseline(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "baseline.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writeBaseline: %v", err)
	}
	return path
}

func TestBaseline_QuarantinedOperation_MarkedAsQuarantinedNotFailed(t *testing.T) {
	dir := t.TempDir()
	path := writeBaseline(t, dir, `
version: 1
quarantined:
  - operation_id: GetOrderBroken
    reason: "tracked in ISSUE-42"
`)

	baseline, err := contract.LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	result := contract.ContractResult{
		Result:     stubResult("GetOrderBroken", false),
		Violations: []ContractViolation{{Field: "status", Severity: Critical}},
	}

	annotated := baseline.Annotate(result)

	if annotated.Failed() {
		t.Error("quarantined result must not count as failed")
	}
	if !annotated.Quarantined {
		t.Error("quarantined result must have Quarantined=true")
	}
	if annotated.QuarantineReason == "" {
		t.Error("QuarantineReason must be non-empty")
	}
}

func TestBaseline_QuarantineExpired_TreatedAsLiveFailure(t *testing.T) {
	dir := t.TempDir()
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	path := writeBaseline(t, dir, `
version: 1
quarantined:
  - operation_id: GetOrderBroken
    reason: "should have been fixed by now"
    quarantine_until: "`+yesterday+`"
`)

	baseline, err := contract.LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	result := contract.ContractResult{
		Result:     stubResult("GetOrderBroken", false),
		Violations: []ContractViolation{{Field: "status", Severity: Critical}},
	}

	annotated := baseline.Annotate(result)

	if !annotated.Failed() {
		t.Error("expired quarantine must treat the result as a live failure")
	}
	if annotated.QuarantineExpired != true {
		t.Error("QuarantineExpired must be true for an expired quarantine")
	}
}

func TestBaseline_StaleEntry_OperationNowPasses_WarningFlagged(t *testing.T) {
	dir := t.TempDir()
	path := writeBaseline(t, dir, `
version: 1
quarantined:
  - operation_id: GetOrderBroken
    reason: "tracked in ISSUE-42"
`)

	baseline, err := contract.LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	passingResult := contract.ContractResult{
		Result:     stubResult("GetOrderBroken", true),
		Violations: nil,
	}

	annotated := baseline.Annotate(passingResult)

	if !annotated.StaleBaseline {
		t.Error("StaleBaseline must be true when a quarantined operation now passes")
	}
}

func TestBaseline_UnknownOperation_NotAffected(t *testing.T) {
	dir := t.TempDir()
	path := writeBaseline(t, dir, `
version: 1
quarantined:
  - operation_id: GetOrderBroken
    reason: "tracked in ISSUE-42"
`)

	baseline, err := contract.LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	result := contract.ContractResult{
		Result:     stubResult("GetHealth", false),
		Violations: []ContractViolation{{Field: "status", Severity: Critical}},
	}

	annotated := baseline.Annotate(result)

	if !annotated.Failed() {
		t.Error("non-quarantined failure must still count as failed")
	}
	if annotated.Quarantined {
		t.Error("non-quarantined result must not be marked Quarantined")
	}
}

func TestBaseline_MissingFile_ReturnsError(t *testing.T) {
	_, err := contract.LoadBaseline("/no/such/file.yaml")
	if err == nil {
		t.Error("expected error for missing baseline file, got nil")
	}
}

func TestBaseline_MalformedYAML_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := writeBaseline(t, dir, `this is not yaml: [`)

	_, err := contract.LoadBaseline(path)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}

// stubResult is a helper for constructing ContractResults in baseline tests.
func stubResult(opID string, passed bool) model.Result {
	return model.Result{OperationID: opID, Passed: passed}
}
```

### Implementation

`internal/strategy/contract/baseline.go` — implements `LoadBaseline`, `Baseline.Annotate`, and the `AnnotatedResult` type.

---

## Step 3 — Exit code model

### Spec

Exit codes are the API surface between `bt` and CI systems. They must be stable, documented, and machine-readable.

| Code | Meaning |
|------|---------|
| `0` | All tests passed (quarantined failures excluded) |
| `1` | One or more contract violations found |
| `2` | Config or schema error — tool misconfigured |
| `3` | Execution error — network failure, server unreachable |

- `ExitCoder` interface with `ExitCode() int`; all `ContractResult` and run-level errors implement it
- The CLI layer maps `error` → exit code using a type switch; it never calls `os.Exit` directly from business logic
- Exit codes are documented in `docs/exit-codes.md`
- Quarantined failures do not affect the exit code

### Tests

`internal/exitcode/exitcode_test.go`:

```go
package exitcode_test

import (
	"errors"
	"testing"

	"github.com/jimbery/bt/internal/exitcode"
	"github.com/jimbery/bt/internal/strategy/contract"
)

func TestExitCode_AllPassed_Zero(t *testing.T) {
	results := []contract.AnnotatedResult{
		{ContractResult: contract.ContractResult{Result: model.Result{Passed: true}}},
	}
	code := exitcode.FromContractResults(results, nil)
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestExitCode_OneFailed_One(t *testing.T) {
	results := []contract.AnnotatedResult{
		{ContractResult: contract.ContractResult{Result: model.Result{Passed: false}}},
	}
	code := exitcode.FromContractResults(results, nil)
	if code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestExitCode_QuarantinedFailure_Zero(t *testing.T) {
	results := []contract.AnnotatedResult{
		{ContractResult: contract.ContractResult{Result: model.Result{Passed: false}}, Quarantined: true},
	}
	code := exitcode.FromContractResults(results, nil)
	if code != 0 {
		t.Errorf("quarantined failure must not affect exit code, got %d", code)
	}
}

func TestExitCode_ConfigError_Two(t *testing.T) {
	code := exitcode.FromContractResults(nil, exitcode.ConfigError("bad config"))
	if code != 2 {
		t.Errorf("expected 2 for config error, got %d", code)
	}
}

func TestExitCode_ExecutionError_Three(t *testing.T) {
	code := exitcode.FromContractResults(nil, exitcode.ExecutionError(errors.New("network down")))
	if code != 3 {
		t.Errorf("expected 3 for execution error, got %d", code)
	}
}
```

### Implementation

`internal/exitcode/exitcode.go`

---

## Step 4 — `bt doctor` command

### Spec

`bt doctor` diagnoses the five most common pre-run failure modes. It runs before any test, exits with code `2` if any check fails, and prints a structured list of checks with PASS / FAIL / WARN indicators.

Checks:

| ID | Name | Passes when |
|----|------|-------------|
| `schema-reachable` | Schema file exists | Schema path in config resolves to a readable file |
| `target-reachable` | Target reachable | HTTP GET to `target.base_url/health` returns within 5 s |
| `auth-configured` | Auth configured | If `auth` block is present, the referenced env var is non-empty |
| `baseline-valid` | Baseline valid | If `.bt/baseline.yaml` exists, it parses without error |
| `go-version` | Go version | `go version` reports ≥ 1.21 |

Output format (console):

```
bt doctor — environment check

  ✓  schema-reachable   Schema found at ./openapi.yaml
  ✓  target-reachable   GET http://localhost:8080/health → 200 in 12ms
  ✗  auth-configured    BT_AUTH_TOKEN is not set (required by config)
  ✓  baseline-valid     .bt/baseline.yaml parsed cleanly (1 quarantined entry)
  ✓  go-version         go1.22.0

1 check failed. Fix the issues above before running bt.
```

### Tests

`internal/doctor/doctor_test.go`:

```go
package doctor_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jimbery/bt/internal/doctor"
)

// --- Schema reachable ---

func TestDoctor_SchemaFileExists_SchemaReachablePasses(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(schemaPath, []byte("openapi: 3.0.0"), 0644); err != nil {
		t.Fatal(err)
	}

	result := doctor.CheckSchemaReachable(schemaPath)

	if !result.Passed {
		t.Errorf("expected check to pass, got: %s", result.Message)
	}
	if result.ID != "schema-reachable" {
		t.Errorf("expected ID 'schema-reachable', got %q", result.ID)
	}
	// Message must confirm what was found, not just say "OK".
	if result.Message == "" {
		t.Error("Message must be non-empty")
	}
}

func TestDoctor_SchemaFileMissing_SchemaReachableFails(t *testing.T) {
	result := doctor.CheckSchemaReachable("/no/such/schema.yaml")

	if result.Passed {
		t.Error("expected check to fail for missing schema file")
	}
	// Message must name the path that was checked.
	if result.Message == "" {
		t.Error("Message must describe what was checked")
	}
}

// --- Target reachable ---

func TestDoctor_TargetReturns200_TargetReachablePasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := doctor.CheckTargetReachable(srv.URL + "/health")

	if !result.Passed {
		t.Errorf("expected check to pass, got: %s", result.Message)
	}
	// Message must include the response time.
	if result.DurationMs <= 0 {
		t.Error("DurationMs must be positive")
	}
}

func TestDoctor_TargetUnreachable_TargetReachableFails(t *testing.T) {
	result := doctor.CheckTargetReachable("http://localhost:19999/health")

	if result.Passed {
		t.Error("expected check to fail for unreachable target")
	}
}

func TestDoctor_TargetReturnsNon200_TargetReachableFailsWithStatusInMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	result := doctor.CheckTargetReachable(srv.URL + "/health")

	if result.Passed {
		t.Error("expected check to fail for non-200 response")
	}
	// Message must report the actual status code received.
	if result.Message == "" {
		t.Error("Message must report what status was received")
	}
}

// --- Auth configured ---

func TestDoctor_AuthEnvVarSet_AuthConfiguredPasses(t *testing.T) {
	t.Setenv("BT_AUTH_TOKEN", "secret")

	result := doctor.CheckAuthConfigured("BT_AUTH_TOKEN")

	if !result.Passed {
		t.Errorf("expected check to pass when env var is set, got: %s", result.Message)
	}
}

func TestDoctor_AuthEnvVarEmpty_AuthConfiguredFails(t *testing.T) {
	t.Setenv("BT_AUTH_TOKEN", "")

	result := doctor.CheckAuthConfigured("BT_AUTH_TOKEN")

	if result.Passed {
		t.Error("expected check to fail when env var is empty")
	}
	// Message must name the env var.
	if result.Message == "" {
		t.Error("Message must name the missing env var")
	}
}

func TestDoctor_AuthEnvVarNotRequired_AuthConfiguredReturnsWarn(t *testing.T) {
	// When auth is not configured in the config, the check should warn, not fail.
	result := doctor.CheckAuthConfigured("") // empty name = no auth required

	if result.Level != doctor.Warn {
		t.Errorf("expected Warn level when auth not required, got %v", result.Level)
	}
}

// --- Baseline valid ---

func TestDoctor_ValidBaselineFile_BaselineValidPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	content := "version: 1\nquarantined:\n  - operation_id: GetOrderBroken\n    reason: \"test\"\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	result := doctor.CheckBaselineValid(path)

	if !result.Passed {
		t.Errorf("expected check to pass for valid baseline, got: %s", result.Message)
	}
	// Message must report how many quarantined entries were found.
	if result.Message == "" {
		t.Error("Message must report quarantine entry count")
	}
}

func TestDoctor_MalformedBaselineFile_BaselineValidFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: ["), 0644); err != nil {
		t.Fatal(err)
	}

	result := doctor.CheckBaselineValid(path)

	if result.Passed {
		t.Error("expected check to fail for malformed baseline")
	}
}

func TestDoctor_BaselineFileMissing_BaselineValidReturnsWarn(t *testing.T) {
	// A missing baseline file is not an error — it just means no quarantines.
	result := doctor.CheckBaselineValid("/no/such/baseline.yaml")

	if result.Level != doctor.Warn {
		t.Errorf("expected Warn for missing baseline file, got %v", result.Level)
	}
}

// --- RunAll ---

func TestDoctor_RunAll_ReturnsOneResultPerCheck(t *testing.T) {
	cfg := doctor.Config{
		SchemaPath:  "/no/such/schema.yaml",
		TargetURL:   "http://localhost:19999/health",
		AuthEnvVar:  "",
		BaselinePath: "/no/such/baseline.yaml",
	}

	results := doctor.RunAll(cfg)

	// There are 5 defined checks; RunAll must return exactly 5 results.
	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
	for _, r := range results {
		if r.ID == "" {
			t.Error("every CheckResult must have a non-empty ID")
		}
		if r.Message == "" {
			t.Error("every CheckResult must have a non-empty Message")
		}
	}
}
```

### Implementation

`internal/doctor/doctor.go` — implements `CheckSchemaReachable`, `CheckTargetReachable`, `CheckAuthConfigured`, `CheckBaselineValid`, `RunAll`, and the console renderer.

`cmd/bt/doctor.go` — wires `bt doctor` to the Cobra command tree.

---

## Step 5 — GitHub Actions install snippet and example workflow

### Spec

- `docs/ci-integration.md` — a prose guide covering: installing `bt` via the GoReleaser-published binary, the example workflow, interpreting exit codes, and uploading JUnit results as GitHub Check annotations
- `.github/workflows/example-bt.yml` — a working example workflow that installs `bt`, runs `bt doctor`, runs `bt run --strategy contract`, and uploads results
- The workflow uses exit code `0` = success, `1` = test gate, `2`/`3` = infrastructure problem; the job step `continue-on-error` is set correctly for each
- The `bt doctor` step runs before `bt run` and fails fast on infrastructure problems (exit `2`/`3`)

No tests for the YAML workflow itself. CI integration is verified in M8.5.

---

## M8 exit criterion

`bt run --strategy contract` verifies provider response schemas field-by-field against the OpenAPI spec and reports violations with full field paths, expected types, and actual values. The baseline model quarantines known failures without hiding them. Structured exit codes allow CI to distinguish test failures from infrastructure errors. `bt doctor` catches the five most common misconfiguration patterns before any test runs. All unit tests pass with `-race`.