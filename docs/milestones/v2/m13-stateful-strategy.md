# M13 — Stateful Strategy

This document follows the project convention: spec first, tests second, implementation third. No implementation file is written until the tests for it exist and clearly fail. The tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

**TDD order for this milestone:**
1. Write and verify all tests in this document (they will fail — no implementation exists yet)
2. Write implementation until all tests pass
3. Proceed to M13.5 integration tests

**ADR references:**
- ADR-010 — Sequence representation (Markov chain → Flow generator)
- ADR-011 — Flow binding model (JSONPath extraction, injection, failure semantics)

> **Package layout is indicative.** Package paths, type names, and file locations are the intended design, not a hard constraint. Implementations may consolidate or restructure packages provided all test assertions and exit criteria are satisfied. The README produced at milestone completion is the authoritative record of what was actually built.

---

## Overview

M13 delivers multi-step flow testing. A `Flow` is a named sequence of `Step` values; each step executes an operation, evaluates assertions, and extracts values from the response that subsequent steps can inject into their inputs.

Flows come from two sources — hand-authored YAML and automatic generation from a `TraceProfile` — but both produce the same internal `Flow` model and run through the same executor.

The six pieces built here:

1. **`Flow` / `FlowResult` model** (`pkg/model/flow.go`) — the shared representation for both sources
2. **Binding engine** (`internal/strategy/stateful/binding`) — JSONPath extraction and injection per ADR-011
3. **Flow YAML loader** (`internal/strategy/stateful/loader`) — parses hand-authored YAML with two-pass validation; catches config errors before any HTTP request is made
4. **Flow generator** (`internal/strategy/stateful/gen`) — produces `[]Flow` from a `TraceProfile` Markov chain per ADR-010
5. **Stateful runner** (`internal/strategy/stateful`) — executes flows, propagates bindings, writes artifacts
6. **MCP tool updates** — `bt_run` accepts `strategy: stateful`; `bt_suggest_strategy` recommends stateful when a trace profile with sequence data is present

**Exit criterion:** `bt run --strategy stateful` executes a hand-authored `create-and-retrieve-order` flow, propagates the `id` binding correctly, reports pass/fail per step with binding values visible. A trace-generated flow runs without error. A step failure produces a replayable artifact. All tests pass with `-race`.

---

## Step 1 — `Flow` and `FlowResult` model

### Spec

- Package: `pkg/model/flow.go`
- `Flow` has: `ID string`, `Description string`, `Steps []FlowStep`
- `FlowStep` has: `ID string`, `OperationID string`, `Input StepInput`, `Expected *StepExpectation`, `Extract map[string]ExtractSpec`
- `StepInput` has: `Method string`, `Path string`, `Headers map[string]string`, `Query map[string]string`, `Body any`
- `StepExpectation` has: `StatusCode int`, `Schema *InlineSchema` (reuses the type from M10)
- `ExtractSpec` has: `From string` (extraction expression), `Into string` (injection target)
- `FlowResult` has: `FlowID string`, `Passed bool`, `Steps []StepResult`, `ArtifactPath string`
- `StepResult` has: `StepID string`, `OperationID string`, `Passed bool`, `StatusCode int`, `Request ResolvedRequest`, `Response StepResponse`, `Bindings map[string]any`, `SchemaViolations []SchemaViolation`, `BindingFailure *BindingFailure`
- `BindingFailure` has: `Key string`, `Expression string`, `Severity string`, `Message string`, `ResponseBody []byte`
- `ResolvedRequest` has: `Method string`, `Path string`, `Headers http.Header`, `QueryParams map[string]string`, `Body []byte`
- `StepResponse` has: `StatusCode int`, `Headers http.Header`, `Body []byte`
- All fields are JSON-serialisable with `omitempty` where nil-safe — `FlowResult` must round-trip through JSON without data loss

### Tests

`pkg/model/flow_test.go`:

```go
package model_test

import (
	"encoding/json"
	"testing"

	"github.com/yourorg/bt/pkg/model"
)

func TestFlowModel_RoundTrip(t *testing.T) {
	original := model.Flow{
		ID:          "create-and-retrieve",
		Description: "Create an order then retrieve it",
		Steps: []model.FlowStep{
			{
				ID:          "create",
				OperationID: "CreateOrder",
				Input: model.StepInput{
					Method: "POST",
					Path:   "/orders",
					Body:   map[string]any{"amount": 100, "currency": "GBP"},
				},
				Expected: &model.StepExpectation{StatusCode: 201},
				Extract: map[string]model.ExtractSpec{
					"order_id": {From: "$.id", Into: "path"},
				},
			},
			{
				ID:          "retrieve",
				OperationID: "GetOrder",
				Input: model.StepInput{
					Method: "GET",
					Path:   "/orders/{order_id}",
				},
				Expected: &model.StepExpectation{StatusCode: 200},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded model.Flow
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	t.Run("flow ID survives round-trip", func(t *testing.T) {
		if decoded.ID != original.ID {
			t.Errorf("want %q, got %q", original.ID, decoded.ID)
		}
	})
	t.Run("step count survives round-trip", func(t *testing.T) {
		if len(decoded.Steps) != len(original.Steps) {
			t.Errorf("want %d steps, got %d", len(original.Steps), len(decoded.Steps))
		}
	})
	t.Run("extract spec survives round-trip", func(t *testing.T) {
		spec := decoded.Steps[0].Extract["order_id"]
		if spec.From != "$.id" {
			t.Errorf("From: want '$.id', got %q", spec.From)
		}
		if spec.Into != "path" {
			t.Errorf("Into: want 'path', got %q", spec.Into)
		}
	})
}

func TestFlowResultModel_RoundTrip(t *testing.T) {
	original := model.FlowResult{
		FlowID: "create-and-retrieve",
		Passed: false,
		Steps: []model.StepResult{
			{
				StepID:      "create",
				OperationID: "CreateOrder",
				Passed:      true,
				StatusCode:  201,
				Bindings:    map[string]any{"order_id": "ord_123"},
				Request: model.ResolvedRequest{
					Method: "POST",
					Path:   "/orders",
					Body:   []byte(`{"amount":100,"currency":"GBP"}`),
				},
				Response: model.StepResponse{
					StatusCode: 201,
					Body:       []byte(`{"id":"ord_123","status":"pending"}`),
				},
				SchemaViolations: []model.SchemaViolation{},
			},
			{
				StepID:      "retrieve",
				OperationID: "GetOrder",
				Passed:      false,
				StatusCode:  404,
				BindingFailure: &model.BindingFailure{
					Key:          "order_id",
					Expression:   "$.id",
					Severity:     "Critical",
					Message:      "path not found in response body",
					ResponseBody: []byte(`{"error":"not found"}`),
				},
			},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded model.FlowResult
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	t.Run("flow result passed=false survives round-trip", func(t *testing.T) {
		if decoded.Passed != false {
			t.Error("Passed should be false")
		}
	})
	t.Run("step count survives round-trip", func(t *testing.T) {
		if len(decoded.Steps) != 2 {
			t.Errorf("want 2 steps, got %d", len(decoded.Steps))
		}
	})
	t.Run("bindings survive round-trip", func(t *testing.T) {
		if decoded.Steps[0].Bindings["order_id"] != "ord_123" {
			t.Errorf("binding order_id: want 'ord_123', got %v", decoded.Steps[0].Bindings["order_id"])
		}
	})
	t.Run("binding failure survives round-trip", func(t *testing.T) {
		bf := decoded.Steps[1].BindingFailure
		if bf == nil {
			t.Fatal("expected non-nil BindingFailure on step 2")
		}
		if bf.Expression != "$.id" {
			t.Errorf("Expression: want '$.id', got %q", bf.Expression)
		}
		if bf.Severity != "Critical" {
			t.Errorf("Severity: want 'Critical', got %q", bf.Severity)
		}
		if len(bf.ResponseBody) == 0 {
			t.Error("ResponseBody must be preserved in BindingFailure")
		}
	})
	t.Run("schema violations is empty slice not nil", func(t *testing.T) {
		// SchemaViolations must be [] not null in JSON
		raw := make(map[string]any)
		json.Unmarshal(data, &raw)
		steps := raw["steps"].([]any)
		step0 := steps[0].(map[string]any)
		violations := step0["schema_violations"]
		arr, ok := violations.([]any)
		if !ok {
			t.Fatalf("expected schema_violations to be array, got %T", violations)
		}
		if len(arr) != 0 {
			t.Errorf("expected empty array, got %v", arr)
		}
	})
}
```

Run and confirm tests fail:

```bash
go test ./pkg/model/... -run "TestFlowModel|TestFlowResultModel" -race -v
```

---

## Step 2 — Binding engine

### Spec

- Package: `internal/strategy/stateful/binding`
- `Extract(expr string, resp StepResponse) (any, error)` — per ADR-011 extraction rules
- `Inject(step *FlowStep, bindings map[string]any) (*ResolvedInput, error)` — applies all bindings in the step's `Extract` map to produce a `ResolvedInput`
- `ValidateExpression(expr string) error` — syntactically validates a JSONPath or named-target expression; used at load time
- Error types: `ErrBindingNotFound` (path absent in response), `ErrBindingTypeMismatch` (value wrong type for target), `ErrConfigError` (expression invalid or binding key undefined)
- `ErrBindingNotFound` message must include the expression that failed
- Extraction rules:
  - `$.field` and deeper — JSONPath via `github.com/PaesslerAG/jsonpath`
  - `$` — entire body parsed as `map[string]any`
  - `header.<name>` — case-insensitive header lookup
  - `status` — HTTP status code as string (e.g. `"201"`)
- Injection rules:
  - `into: path` — replace `{key}` in `StepInput.Path` with the extracted string value
  - `into: query.<name>` — add/replace query parameter
  - `into: header.<name>` — add/replace request header
  - `into: body` — replace entire body; only valid when `from` is `$`
  - `into: body.<jsonpath>` — set a specific field in an existing body map
- `$` extraction with any `into` target other than `body` is `ErrConfigError`
- Multi-value JSONPath results (arrays from `[*]` expressions) with any `into` target other than `body` are `ErrConfigError`

### Tests

`internal/strategy/stateful/binding/binding_test.go`:

```go
package binding_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/yourorg/bt/internal/strategy/stateful/binding"
	"github.com/yourorg/bt/pkg/model"
)

func respWith(body string, status int, headers map[string]string) model.StepResponse {
	h := make(http.Header)
	for k, v := range headers {
		h.Set(k, v)
	}
	return model.StepResponse{
		StatusCode: status,
		Body:       []byte(body),
		Headers:    h,
	}
}

var orderResp = respWith(
	`{"id":"ord_123","status":"pending","items":[{"sku":"A1"},{"sku":"B2"}]}`,
	201,
	map[string]string{"X-Request-Id": "req_abc", "Content-Type": "application/json"},
)

// --- Extract: JSONPath ---

func TestExtract_JSONPath_TopLevelField(t *testing.T) {
	val, err := binding.Extract("$.id", orderResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "ord_123" {
		t.Errorf("want 'ord_123', got %v", val)
	}
}

func TestExtract_JSONPath_NestedArrayElement(t *testing.T) {
	val, err := binding.Extract("$.items[0].sku", orderResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "A1" {
		t.Errorf("want 'A1', got %v", val)
	}
}

func TestExtract_JSONPath_SecondArrayElement(t *testing.T) {
	val, err := binding.Extract("$.items[1].sku", orderResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "B2" {
		t.Errorf("want 'B2', got %v", val)
	}
}

func TestExtract_JSONPath_PathNotFound_ReturnsErrBindingNotFound(t *testing.T) {
	_, err := binding.Extract("$.nonexistent", orderResp)
	if err == nil {
		t.Fatal("expected error for missing path")
	}
	if !binding.IsErrBindingNotFound(err) {
		t.Errorf("expected ErrBindingNotFound, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "$.nonexistent") {
		t.Errorf("error must mention the expression; got: %v", err)
	}
}

func TestExtract_JSONPath_NestedPathNotFound_ReturnsErrBindingNotFound(t *testing.T) {
	_, err := binding.Extract("$.items[0].missing", orderResp)
	if !binding.IsErrBindingNotFound(err) {
		t.Errorf("expected ErrBindingNotFound for missing nested field, got %T: %v", err, err)
	}
}

func TestExtract_DollarSign_ReturnsEntireBodyAsMap(t *testing.T) {
	val, err := binding.Extract("$", orderResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", val)
	}
	if m["id"] != "ord_123" {
		t.Errorf("body map id: want 'ord_123', got %v", m["id"])
	}
}

// --- Extract: header ---

func TestExtract_Header_CaseInsensitiveMatch(t *testing.T) {
	val, err := binding.Extract("header.x-request-id", orderResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "req_abc" {
		t.Errorf("want 'req_abc', got %v", val)
	}
}

func TestExtract_Header_UppercaseExpression(t *testing.T) {
	val, err := binding.Extract("header.X-REQUEST-ID", orderResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "req_abc" {
		t.Errorf("want 'req_abc', got %v", val)
	}
}

func TestExtract_Header_AbsentHeader_ReturnsErrBindingNotFound(t *testing.T) {
	_, err := binding.Extract("header.X-Does-Not-Exist", orderResp)
	if !binding.IsErrBindingNotFound(err) {
		t.Errorf("expected ErrBindingNotFound for absent header, got %T: %v", err, err)
	}
}

// --- Extract: status ---

func TestExtract_Status_ReturnsStatusAsString(t *testing.T) {
	val, err := binding.Extract("status", orderResp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "201" {
		t.Errorf("want '201', got %v", val)
	}
}

func TestExtract_Status_ZeroStatus_ReturnsZeroString(t *testing.T) {
	resp := respWith(`{}`, 0, nil)
	val, err := binding.Extract("status", resp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "0" {
		t.Errorf("want '0', got %v", val)
	}
}

// --- Inject: path ---

func TestInject_Path_ReplacesSinglePlaceholder(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{Method: "GET", Path: "/orders/{order_id}"},
		Extract: map[string]model.ExtractSpec{
			"order_id": {From: "$.id", Into: "path"},
		},
	}
	bindings := map[string]any{"order_id": "ord_123"}
	resolved, err := binding.Inject(step, bindings)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if resolved.Path != "/orders/ord_123" {
		t.Errorf("Path: want '/orders/ord_123', got %q", resolved.Path)
	}
}

func TestInject_Path_MultipleBindingsInPath(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{Method: "GET", Path: "/orgs/{org_id}/orders/{order_id}"},
		Extract: map[string]model.ExtractSpec{
			"org_id":   {From: "$.org_id", Into: "path"},
			"order_id": {From: "$.id", Into: "path"},
		},
	}
	bindings := map[string]any{"org_id": "org_1", "order_id": "ord_123"}
	resolved, err := binding.Inject(step, bindings)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if resolved.Path != "/orgs/org_1/orders/ord_123" {
		t.Errorf("Path: want '/orgs/org_1/orders/ord_123', got %q", resolved.Path)
	}
}

func TestInject_Path_NonStringValue_ReturnsErrBindingTypeMismatch(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{Path: "/orders/{order_id}"},
		Extract: map[string]model.ExtractSpec{
			"order_id": {From: "$.amount", Into: "path"},
		},
	}
	// amount is a number — not stringifiable as a path param cleanly
	bindings := map[string]any{"order_id": map[string]any{"nested": "object"}}
	_, err := binding.Inject(step, bindings)
	if !binding.IsErrBindingTypeMismatch(err) {
		t.Errorf("expected ErrBindingTypeMismatch for object injected into path, got %T: %v", err, err)
	}
}

// --- Inject: query ---

func TestInject_Query_AddsSingleParameter(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{Method: "GET", Path: "/orders"},
		Extract: map[string]model.ExtractSpec{
			"cursor": {From: "$.next_cursor", Into: "query.cursor"},
		},
	}
	bindings := map[string]any{"cursor": "tok_xyz"}
	resolved, err := binding.Inject(step, bindings)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if resolved.QueryParams["cursor"] != "tok_xyz" {
		t.Errorf("QueryParams[cursor]: want 'tok_xyz', got %q", resolved.QueryParams["cursor"])
	}
}

func TestInject_Query_PreservesExistingQueryParams(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{
			Method: "GET",
			Path:   "/orders",
			Query:  map[string]string{"status": "pending"},
		},
		Extract: map[string]model.ExtractSpec{
			"cursor": {From: "$.next_cursor", Into: "query.cursor"},
		},
	}
	bindings := map[string]any{"cursor": "tok_xyz"}
	resolved, err := binding.Inject(step, bindings)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if resolved.QueryParams["status"] != "pending" {
		t.Errorf("existing query param 'status' should be preserved, got %q", resolved.QueryParams["status"])
	}
	if resolved.QueryParams["cursor"] != "tok_xyz" {
		t.Errorf("injected query param 'cursor': want 'tok_xyz', got %q", resolved.QueryParams["cursor"])
	}
}

// --- Inject: header ---

func TestInject_Header_AddsHeader(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{Method: "GET", Path: "/orders"},
		Extract: map[string]model.ExtractSpec{
			"auth_token": {From: "header.X-Auth-Token", Into: "header.Authorization"},
		},
	}
	bindings := map[string]any{"auth_token": "Bearer abc123"}
	resolved, err := binding.Inject(step, bindings)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if resolved.Headers.Get("Authorization") != "Bearer abc123" {
		t.Errorf("Authorization header: want 'Bearer abc123', got %q", resolved.Headers.Get("Authorization"))
	}
}

// --- Inject: body ---

func TestInject_Body_ReplacesEntireBody(t *testing.T) {
	bodyMap := map[string]any{"id": "ord_123", "status": "pending"}
	step := &model.FlowStep{
		Input: model.StepInput{Method: "POST", Path: "/orders"},
		Extract: map[string]model.ExtractSpec{
			"order_body": {From: "$", Into: "body"},
		},
	}
	bindings := map[string]any{"order_body": bodyMap}
	resolved, err := binding.Inject(step, bindings)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if resolved.Body == nil {
		t.Fatal("expected non-nil body after body injection")
	}
}

// --- ValidateExpression ---

func TestValidateExpression_ValidJSONPath_NoError(t *testing.T) {
	validExprs := []string{"$.id", "$.items[0].sku", "$.data.order.id", "$"}
	for _, expr := range validExprs {
		t.Run(expr, func(t *testing.T) {
			if err := binding.ValidateExpression(expr); err != nil {
				t.Errorf("expected no error for %q, got: %v", expr, err)
			}
		})
	}
}

func TestValidateExpression_ValidNamedTargets_NoError(t *testing.T) {
	validExprs := []string{"status", "header.Location", "header.X-Request-ID"}
	for _, expr := range validExprs {
		t.Run(expr, func(t *testing.T) {
			if err := binding.ValidateExpression(expr); err != nil {
				t.Errorf("expected no error for %q, got: %v", expr, err)
			}
		})
	}
}

func TestValidateExpression_InvalidJSONPath_ReturnsError(t *testing.T) {
	invalidExprs := []string{"$..[invalid", "not-a-path", ""}
	for _, expr := range invalidExprs {
		t.Run(expr, func(t *testing.T) {
			err := binding.ValidateExpression(expr)
			if err == nil {
				t.Errorf("expected error for invalid expression %q", expr)
			}
		})
	}
}

// --- Config error: $ with non-body into ---

func TestExtract_DollarSign_WithPathInto_IsConfigError(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{Path: "/orders/{order_body}"},
		Extract: map[string]model.ExtractSpec{
			"order_body": {From: "$", Into: "path"}, // invalid — $ can only go into body
		},
	}
	_, err := binding.Inject(step, map[string]any{"order_body": map[string]any{}})
	if !binding.IsErrConfigError(err) {
		t.Errorf("expected ErrConfigError for $ with into:path, got %T: %v", err, err)
	}
}
```

Run and confirm tests fail:

```bash
go test ./internal/strategy/stateful/binding/... -race -v
```

---

## Step 3 — Flow YAML loader

### Spec

- Package: `internal/strategy/stateful/loader`
- `LoadFlow(r io.Reader) (*model.Flow, error)` — parses a single flow from YAML
- `LoadFlowFile(path string) (*model.Flow, error)` — reads and parses from a file path
- `LoadFlowsDir(dir string) ([]model.Flow, error)` — loads all `*.yaml` and `*.yml` files in the directory; returns all valid flows; first parse error halts and returns the error
- Two-pass validation:
  - **Pass 1 — syntactic:** every `extract[*].from` expression is validated via `binding.ValidateExpression`; invalid expressions → `ErrConfigError` naming the expression
  - **Pass 2 — semantic:** every `{key}` placeholder in `input.path` and every `{key}` reference in query/header values must correspond to an `extract` key defined in the **same or earlier** step; forward references → `ErrConfigError` naming the key
- `$` extraction with any `into` target other than `body` → `ErrConfigError` at load time
- `IsConfigError(err error) bool` — sentinel check

### Tests

`internal/strategy/stateful/loader/loader_test.go`:

```go
package loader_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/bt/internal/strategy/stateful/loader"
)

// --- YAML fixtures ---

const validFlowYAML = `
flows:
  - id: create-and-retrieve
    description: "Create an order then retrieve it"
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
          body:
            amount: 100
            currency: GBP
        expected:
          status_code: 201
          schema:
            type: object
            required: [id, status]
            properties:
              id:     { type: string }
              status: { type: string }
        extract:
          order_id:
            from: "$.id"
            into: path
      - id: retrieve
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{order_id}"
        expected:
          status_code: 200
`

const flowWithInvalidJSONPath = `
flows:
  - id: bad-extract
    steps:
      - id: step1
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
        extract:
          bad_key:
            from: "$..[invalid syntax"
            into: path
`

const flowWithUndefinedBinding = `
flows:
  - id: undefined-ref
    steps:
      - id: step1
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{undefined_key}"
`

const flowWithForwardBinding = `
flows:
  - id: forward-ref
    steps:
      - id: step1
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{order_id}"
      - id: step2
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
        extract:
          order_id:
            from: "$.id"
            into: path
`

const flowWithDollarIntoPath = `
flows:
  - id: dollar-into-path
    steps:
      - id: step1
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
        extract:
          whole_body:
            from: "$"
            into: path
`

const multipleFlowsYAML = `
flows:
  - id: flow-one
    steps:
      - id: step1
        operation_id: GetHealth
        input:
          method: GET
          path: /health
        expected:
          status_code: 200
  - id: flow-two
    steps:
      - id: step1
        operation_id: ListOrders
        input:
          method: GET
          path: /orders
        expected:
          status_code: 200
`

// --- LoadFlow: valid ---

func TestLoadFlow_ValidYAML_ParsedCorrectly(t *testing.T) {
	flow, err := loader.LoadFlow(strings.NewReader(validFlowYAML))
	if err != nil {
		t.Fatalf("LoadFlow: %v", err)
	}

	t.Run("flow ID is set", func(t *testing.T) {
		if flow.ID != "create-and-retrieve" {
			t.Errorf("want 'create-and-retrieve', got %q", flow.ID)
		}
	})
	t.Run("step count is 2", func(t *testing.T) {
		if len(flow.Steps) != 2 {
			t.Fatalf("want 2 steps, got %d", len(flow.Steps))
		}
	})
	t.Run("first step operation ID", func(t *testing.T) {
		if flow.Steps[0].OperationID != "CreateOrder" {
			t.Errorf("want 'CreateOrder', got %q", flow.Steps[0].OperationID)
		}
	})
	t.Run("extract spec on first step", func(t *testing.T) {
		spec, ok := flow.Steps[0].Extract["order_id"]
		if !ok {
			t.Fatal("expected extract key 'order_id'")
		}
		if spec.From != "$.id" {
			t.Errorf("From: want '$.id', got %q", spec.From)
		}
		if spec.Into != "path" {
			t.Errorf("Into: want 'path', got %q", spec.Into)
		}
	})
	t.Run("second step path contains binding placeholder", func(t *testing.T) {
		if flow.Steps[1].Input.Path != "/orders/{order_id}" {
			t.Errorf("Path: want '/orders/{order_id}', got %q", flow.Steps[1].Input.Path)
		}
	})
	t.Run("expected status code on first step", func(t *testing.T) {
		if flow.Steps[0].Expected == nil || flow.Steps[0].Expected.StatusCode != 201 {
			t.Error("expected status code 201 on create step")
		}
	})
	t.Run("body on first step", func(t *testing.T) {
		if flow.Steps[0].Input.Body == nil {
			t.Error("expected non-nil body on create step")
		}
	})
}

func TestLoadFlow_NoDescription_ParsesWithoutError(t *testing.T) {
	yaml := `
flows:
  - id: minimal
    steps:
      - id: s1
        operation_id: GetHealth
        input:
          method: GET
          path: /health
`
	flow, err := loader.LoadFlow(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("expected no error for flow without description: %v", err)
	}
	if flow.Description != "" {
		t.Errorf("expected empty description, got %q", flow.Description)
	}
}

func TestLoadFlow_MultipleFlows_ReturnsFirst(t *testing.T) {
	// LoadFlow returns the first flow in the YAML; LoadFlowsDir loads all
	flow, err := loader.LoadFlow(strings.NewReader(multipleFlowsYAML))
	if err != nil {
		t.Fatalf("LoadFlow: %v", err)
	}
	if flow.ID != "flow-one" {
		t.Errorf("want 'flow-one', got %q", flow.ID)
	}
}

// --- LoadFlow: config errors at load time ---

func TestLoadFlow_InvalidJSONPath_ReturnsErrConfigError(t *testing.T) {
	_, err := loader.LoadFlow(strings.NewReader(flowWithInvalidJSONPath))
	if err == nil {
		t.Fatal("expected ErrConfigError for invalid JSONPath expression")
	}
	if !loader.IsConfigError(err) {
		t.Errorf("expected ErrConfigError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("error should mention 'invalid'; got: %v", err)
	}
}

func TestLoadFlow_UndefinedBindingKey_ReturnsErrConfigError(t *testing.T) {
	_, err := loader.LoadFlow(strings.NewReader(flowWithUndefinedBinding))
	if err == nil {
		t.Fatal("expected ErrConfigError for undefined binding key")
	}
	if !loader.IsConfigError(err) {
		t.Errorf("expected ErrConfigError, got %T: %v", err, err)
	}
	if !strings.Contains(err.Error(), "undefined_key") {
		t.Errorf("error should name the undefined key; got: %v", err)
	}
}

func TestLoadFlow_ForwardReference_ReturnsErrConfigError(t *testing.T) {
	_, err := loader.LoadFlow(strings.NewReader(flowWithForwardBinding))
	if err == nil {
		t.Fatal("expected ErrConfigError for forward binding reference")
	}
	if !loader.IsConfigError(err) {
		t.Errorf("expected ErrConfigError for forward ref, got %T: %v", err, err)
	}
}

func TestLoadFlow_DollarSignIntoPath_ReturnsErrConfigError(t *testing.T) {
	_, err := loader.LoadFlow(strings.NewReader(flowWithDollarIntoPath))
	if err == nil {
		t.Fatal("expected ErrConfigError for $ with into:path")
	}
	if !loader.IsConfigError(err) {
		t.Errorf("expected ErrConfigError, got %T: %v", err, err)
	}
}

func TestLoadFlow_InvalidYAML_ReturnsError(t *testing.T) {
	_, err := loader.LoadFlow(strings.NewReader("not: valid: yaml: :::"))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// --- LoadFlowsDir ---

func TestLoadFlowsDir_MultipleFiles_LoadsAll(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(`
flows:
  - id: flow-a
    steps:
      - id: s1
        operation_id: GetHealth
        input: {method: GET, path: /health}
`), 0644)
	os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(`
flows:
  - id: flow-b
    steps:
      - id: s1
        operation_id: ListOrders
        input: {method: GET, path: /orders}
`), 0644)

	flows, err := loader.LoadFlowsDir(dir)
	if err != nil {
		t.Fatalf("LoadFlowsDir: %v", err)
	}
	if len(flows) != 2 {
		t.Errorf("expected 2 flows, got %d", len(flows))
	}
}

func TestLoadFlowsDir_EmptyDir_ReturnsEmptySlice(t *testing.T) {
	dir := t.TempDir()
	flows, err := loader.LoadFlowsDir(dir)
	if err != nil {
		t.Fatalf("unexpected error for empty dir: %v", err)
	}
	if len(flows) != 0 {
		t.Errorf("expected 0 flows for empty dir, got %d", len(flows))
	}
}

func TestLoadFlowsDir_IgnoresNonYAMLFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# ignore me"), 0644)
	os.WriteFile(filepath.Join(dir, "flow.yaml"), []byte(`
flows:
  - id: only-flow
    steps:
      - id: s1
        operation_id: GetHealth
        input: {method: GET, path: /health}
`), 0644)

	flows, err := loader.LoadFlowsDir(dir)
	if err != nil {
		t.Fatalf("LoadFlowsDir: %v", err)
	}
	if len(flows) != 1 {
		t.Errorf("expected 1 flow (ignoring .md file), got %d", len(flows))
	}
}

func TestLoadFlowsDir_InvalidFileHaltsWithError(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(`
flows:
  - id: bad
    steps:
      - id: s1
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{undefined_key}"
`), 0644)

	_, err := loader.LoadFlowsDir(dir)
	if err == nil {
		t.Fatal("expected error when a flow file has a config error")
	}
	if !loader.IsConfigError(err) {
		t.Errorf("expected ErrConfigError from LoadFlowsDir, got %T: %v", err, err)
	}
}
```

Run and confirm tests fail:

```bash
go test ./internal/strategy/stateful/loader/... -race -v
```

---

## Step 4 — Flow generator from TraceProfile

### Spec

- Package: `internal/strategy/stateful/gen`
- `GenerateFlows(profile *model.TraceProfile, cfg GenerateFlowsConfig) []model.Flow`
- `GenerateFlowsConfig` has: `Count int` (number of flows to generate), `MaxSteps int` (cap per flow, default `profile.Sequences.MaxObservedSessionLength`, hard cap 10)
- Each flow is generated by Markov sampling:
  1. Sample a start operation from `profile.Sequences.StartProbability`
  2. Follow `profile.Sequences.Transitions` until `__END__` is sampled or `MaxSteps` is reached
  3. Each sampled operation becomes a `FlowStep` with no `Extract` or `Expected` — trace-generated flows are bare skeletons; assertions are added by teams if they want them
- Flow IDs are generated as `trace-flow-{n}` (1-indexed)
- `GenerateFlows` with a nil or empty profile returns `[]model.Flow{}` (empty, not nil)
- Flows must not all be identical over a `Count` of 20 or more (randomness is live, not seeded here — determinism is a concern for the runner, not the generator)

### Tests

`internal/strategy/stateful/gen/gen_test.go`:

```go
package gen_test

import (
	"testing"

	"github.com/yourorg/bt/internal/strategy/stateful/gen"
	"github.com/yourorg/bt/pkg/model"
)

func ordersProfile() *model.TraceProfile {
	return &model.TraceProfile{
		SchemaVersion: "1",
		Operations: map[string]*model.OperationProfile{
			"CreateOrder": {CallCount: 100, FrequencyRank: 1},
			"GetOrder":    {CallCount: 90, FrequencyRank: 2},
			"ListOrders":  {CallCount: 20, FrequencyRank: 3},
		},
		Sequences: &model.SequenceProfile{
			StartProbability: map[string]float64{
				"CreateOrder": 0.85,
				"ListOrders":  0.15,
			},
			Transitions: map[string]map[string]float64{
				"CreateOrder": {"GetOrder": 0.87, "ListOrders": 0.08, "__END__": 0.05},
				"GetOrder":    {"__END__": 0.70, "GetOrder": 0.10, "ListOrders": 0.20},
				"ListOrders":  {"__END__": 0.80, "CreateOrder": 0.20},
			},
			MinObservedSessionLength: 1,
			MaxObservedSessionLength: 5,
		},
	}
}

func TestGenerateFlows_ProducesRequestedCount(t *testing.T) {
	flows := gen.GenerateFlows(ordersProfile(), gen.GenerateFlowsConfig{Count: 10, MaxSteps: 5})
	if len(flows) != 10 {
		t.Errorf("want 10 flows, got %d", len(flows))
	}
}

func TestGenerateFlows_EachFlowHasID(t *testing.T) {
	flows := gen.GenerateFlows(ordersProfile(), gen.GenerateFlowsConfig{Count: 5, MaxSteps: 5})
	seen := map[string]bool{}
	for _, f := range flows {
		if f.ID == "" {
			t.Error("flow ID must not be empty")
		}
		if seen[f.ID] {
			t.Errorf("duplicate flow ID: %q", f.ID)
		}
		seen[f.ID] = true
	}
}

func TestGenerateFlows_EachFlowStartsWithValidOperation(t *testing.T) {
	profile := ordersProfile()
	flows := gen.GenerateFlows(profile, gen.GenerateFlowsConfig{Count: 50, MaxSteps: 5})
	validStarts := map[string]bool{}
	for op := range profile.Sequences.StartProbability {
		validStarts[op] = true
	}
	for _, f := range flows {
		if len(f.Steps) == 0 {
			t.Errorf("flow %q has no steps", f.ID)
			continue
		}
		if !validStarts[f.Steps[0].OperationID] {
			t.Errorf("flow %q starts with %q which is not in StartProbability", f.ID, f.Steps[0].OperationID)
		}
	}
}

func TestGenerateFlows_NoFlowExceedsMaxSteps(t *testing.T) {
	flows := gen.GenerateFlows(ordersProfile(), gen.GenerateFlowsConfig{Count: 100, MaxSteps: 3})
	for _, f := range flows {
		if len(f.Steps) > 3 {
			t.Errorf("flow %q has %d steps, exceeds MaxSteps=3", f.ID, len(f.Steps))
		}
	}
}

func TestGenerateFlows_HardCapAtTen(t *testing.T) {
	// MaxSteps > 10 must be capped at 10
	flows := gen.GenerateFlows(ordersProfile(), gen.GenerateFlowsConfig{Count: 20, MaxSteps: 100})
	for _, f := range flows {
		if len(f.Steps) > 10 {
			t.Errorf("flow %q has %d steps, hard cap of 10 violated", f.ID, len(f.Steps))
		}
	}
}

func TestGenerateFlows_AllOperationIDsExistInProfile(t *testing.T) {
	profile := ordersProfile()
	flows := gen.GenerateFlows(profile, gen.GenerateFlowsConfig{Count: 50, MaxSteps: 5})
	for _, f := range flows {
		for _, step := range f.Steps {
			if _, ok := profile.Operations[step.OperationID]; !ok {
				t.Errorf("flow %q step %q references operation %q not in profile",
					f.ID, step.ID, step.OperationID)
			}
		}
	}
}

func TestGenerateFlows_FlowsAreNotAllIdentical(t *testing.T) {
	flows := gen.GenerateFlows(ordersProfile(), gen.GenerateFlowsConfig{Count: 20, MaxSteps: 5})
	signatures := map[string]bool{}
	for _, f := range flows {
		sig := flowSignature(f)
		signatures[sig] = true
	}
	if len(signatures) < 2 {
		t.Errorf("all 20 flows are identical — generator is not sampling from the Markov chain: %v", signatures)
	}
}

func TestGenerateFlows_NilProfile_ReturnsEmptySlice(t *testing.T) {
	flows := gen.GenerateFlows(nil, gen.GenerateFlowsConfig{Count: 10})
	if flows == nil {
		t.Error("expected empty slice, not nil")
	}
	if len(flows) != 0 {
		t.Errorf("expected 0 flows for nil profile, got %d", len(flows))
	}
}

func TestGenerateFlows_EmptySequences_ReturnsEmptySlice(t *testing.T) {
	profile := &model.TraceProfile{
		SchemaVersion: "1",
		Operations:    map[string]*model.OperationProfile{},
		Sequences:     nil,
	}
	flows := gen.GenerateFlows(profile, gen.GenerateFlowsConfig{Count: 10})
	if len(flows) != 0 {
		t.Errorf("expected 0 flows for profile with nil Sequences, got %d", len(flows))
	}
}

func TestGenerateFlows_DefaultMaxStepsFromProfile(t *testing.T) {
	// When MaxSteps is 0, it should default to profile.Sequences.MaxObservedSessionLength
	profile := ordersProfile() // MaxObservedSessionLength = 5
	flows := gen.GenerateFlows(profile, gen.GenerateFlowsConfig{Count: 50, MaxSteps: 0})
	for _, f := range flows {
		if len(f.Steps) > 5 {
			t.Errorf("flow %q has %d steps; expected default cap of 5", f.ID, len(f.Steps))
		}
	}
}

// --- helper ---

func flowSignature(f model.Flow) string {
	sig := f.ID + ":"
	for _, s := range f.Steps {
		sig += s.OperationID + ">"
	}
	return sig
}
```

Run and confirm tests fail:

```bash
go test ./internal/strategy/stateful/gen/... -race -v
```

---

## Step 5 — Stateful runner

### Spec

- Package: `internal/strategy/stateful`
- `Runner` implements `Strategy`
- `Execute(ctx context.Context, flows []model.Flow, exec runner.Executor) ([]model.FlowResult, error)`
- Per flow:
  1. Accumulate a `bindings map[string]any` scoped to the flow (starts empty)
  2. For each step in order:
     a. Call `binding.Inject(step, bindings)` to produce `ResolvedInput`
     b. Execute the HTTP request via `exec`
     c. Evaluate `step.Expected.StatusCode` if set; failure does **not** halt the step
     d. Evaluate `step.Expected.Schema` if set using `SchemaAssertion.Evaluate` from M10; violations are collected but do **not** halt the step
     e. For each `extract` spec, call `binding.Extract(spec.From, response)`; on `ErrBindingNotFound`, record a `BindingFailure`, mark the step failed, halt the flow — do not execute further steps
     f. On successful extraction, add to `bindings`
  3. A flow `Passed` iff all steps passed (no status code failures, no schema violations, no binding failures)
- `FlowResult.Steps` always contains all steps that were attempted, not just the failures
- If a step is not attempted (flow halted at a previous step), it does not appear in `FlowResult.Steps`
- On any step failure or binding failure the runner writes an artifact bundle to `ArtifactDir` (if configured); the bundle includes all attempted steps with their requests, responses, and binding values
- `bt replay <artifact>` re-executes all steps, substituting recorded binding values instead of re-extracting — ensures deterministic replay even if the server assigns different IDs

### Tests

`internal/strategy/stateful/runner_test.go`:

```go
package stateful_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/yourorg/bt/internal/strategy/stateful"
	"github.com/yourorg/bt/internal/strategy/stateful/loader"
	"github.com/yourorg/bt/pkg/model"
)

// --- server helpers ---

// twoStepServer: first request returns {"id":"ord_123","status":"pending"},
// second request to /orders/ord_123 returns {"id":"ord_123","amount":100,"status":"pending"}
func twoStepServer(t *testing.T) *httptest.Server {
	t.Helper()
	var count int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(201)
			w.Write([]byte(`{"id":"ord_123","status":"pending","amount":100,"currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		} else {
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"ord_123","amount":100,"currency":"GBP","status":"pending","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
}

// bindingMissingServer: always returns a body without the 'id' field
func bindingMissingServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		w.Write([]byte(`{"status":"pending"}`)) // no 'id' field
	}))
}

// wrongStatusServer: always returns 500
func wrongStatusServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"internal","code":"INTERNAL_ERROR"}`))
	}))
}

func mustLoadFlow(t *testing.T, yaml string) model.Flow {
	t.Helper()
	flow, err := loader.LoadFlow(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("LoadFlow: %v", err)
	}
	return *flow
}

const createAndRetrieveYAML = `
flows:
  - id: create-and-retrieve
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
          body:
            amount: 100
            currency: GBP
        expected:
          status_code: 201
        extract:
          order_id:
            from: "$.id"
            into: path
      - id: retrieve
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{order_id}"
        expected:
          status_code: 200
`

// --- Execute: happy path ---

func TestRunner_HappyPath_BothStepsPass(t *testing.T) {
	srv := twoStepServer(t)
	defer srv.Close()

	flow := mustLoadFlow(t, createAndRetrieveYAML)
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, err := runner.Execute(context.Background(), []model.Flow{flow}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 FlowResult, got %d", len(results))
	}
	result := results[0]

	t.Run("flow passes", func(t *testing.T) {
		if !result.Passed {
			t.Errorf("expected flow to pass; steps: %v", result.Steps)
		}
	})
	t.Run("both steps present in result", func(t *testing.T) {
		if len(result.Steps) != 2 {
			t.Errorf("expected 2 step results, got %d", len(result.Steps))
		}
	})
	t.Run("binding value recorded in create step", func(t *testing.T) {
		if result.Steps[0].Bindings["order_id"] != "ord_123" {
			t.Errorf("expected binding order_id='ord_123', got %v", result.Steps[0].Bindings["order_id"])
		}
	})
	t.Run("retrieve step path has binding injected", func(t *testing.T) {
		if result.Steps[1].Request.Path != "/orders/ord_123" {
			t.Errorf("expected path '/orders/ord_123', got %q", result.Steps[1].Request.Path)
		}
	})
}

// --- Execute: binding propagates across steps ---

func TestRunner_BindingFromEarlierStep_UsedInLaterStep(t *testing.T) {
	// 3-step flow: create → patch (uses order_id) → retrieve (also uses order_id)
	const threeStepYAML = `
flows:
  - id: three-step
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
          body: {amount: 100, currency: GBP}
        expected: {status_code: 201}
        extract:
          order_id: {from: "$.id", into: path}
      - id: patch
        operation_id: PatchOrder
        input:
          method: PATCH
          path: "/orders/{order_id}"
          body: {status: confirmed}
        expected: {status_code: 200}
      - id: retrieve
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{order_id}"
        expected: {status_code: 200}
`
	var callCount int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(201)
			w.Write([]byte(`{"id":"ord_999","status":"pending","amount":100,"currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		} else {
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"ord_999","status":"confirmed","amount":100,"currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
	defer srv.Close()

	flow := mustLoadFlow(t, threeStepYAML)
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, _ := runner.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	t.Run("patch step uses order_id from create step", func(t *testing.T) {
		if result.Steps[1].Request.Path != "/orders/ord_999" {
			t.Errorf("patch path: want '/orders/ord_999', got %q", result.Steps[1].Request.Path)
		}
	})
	t.Run("retrieve step also uses order_id from create step", func(t *testing.T) {
		if result.Steps[2].Request.Path != "/orders/ord_999" {
			t.Errorf("retrieve path: want '/orders/ord_999', got %q", result.Steps[2].Request.Path)
		}
	})
}

// --- Execute: binding failure halts flow ---

func TestRunner_BindingFailure_HaltsFlow(t *testing.T) {
	srv := bindingMissingServer(t)
	defer srv.Close()

	flow := mustLoadFlow(t, createAndRetrieveYAML)
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, _ := runner.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	t.Run("flow fails", func(t *testing.T) {
		if result.Passed {
			t.Error("expected flow to fail when binding extraction fails")
		}
	})
	t.Run("only one step in result (flow halted)", func(t *testing.T) {
		if len(result.Steps) != 1 {
			t.Errorf("expected 1 step result (halted after step 1), got %d", len(result.Steps))
		}
	})
	t.Run("step has BindingFailure set", func(t *testing.T) {
		if result.Steps[0].BindingFailure == nil {
			t.Fatal("expected BindingFailure on first step")
		}
	})
	t.Run("binding failure expression is $.id", func(t *testing.T) {
		if result.Steps[0].BindingFailure.Expression != "$.id" {
			t.Errorf("Expression: want '$.id', got %q", result.Steps[0].BindingFailure.Expression)
		}
	})
	t.Run("binding failure severity is Critical", func(t *testing.T) {
		if result.Steps[0].BindingFailure.Severity != "Critical" {
			t.Errorf("Severity: want 'Critical', got %q", result.Steps[0].BindingFailure.Severity)
		}
	})
	t.Run("binding failure includes response body", func(t *testing.T) {
		if len(result.Steps[0].BindingFailure.ResponseBody) == 0 {
			t.Error("BindingFailure.ResponseBody must be non-empty for diagnosis")
		}
	})
}

// --- Execute: status code failure does not halt ---

func TestRunner_StatusCodeFailure_DoesNotHaltFlow(t *testing.T) {
	// Both steps hit the server; step 1 gets a 500 but flow continues to step 2
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// Step 1: returns 500 but includes 'id' so binding can still be extracted
			w.WriteHeader(500)
			w.Write([]byte(`{"id":"ord_123","error":"something failed"}`))
		} else {
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"ord_123","amount":100,"status":"pending","currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
	defer srv.Close()

	flow := mustLoadFlow(t, createAndRetrieveYAML)
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, _ := runner.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	t.Run("flow fails overall", func(t *testing.T) {
		if result.Passed {
			t.Error("expected flow to fail (step 1 got 500)")
		}
	})
	t.Run("both steps are present in result (flow not halted by status failure)", func(t *testing.T) {
		if len(result.Steps) != 2 {
			t.Errorf("expected 2 step results (status failure does not halt), got %d", len(result.Steps))
		}
	})
	t.Run("step 1 failed", func(t *testing.T) {
		if result.Steps[0].Passed {
			t.Error("expected step 1 to fail (status 500 != 201)")
		}
	})
	t.Run("step 2 attempted and passed", func(t *testing.T) {
		if !result.Steps[1].Passed {
			t.Error("expected step 2 to pass (status 200 == 200)")
		}
	})
}

// --- Execute: schema violation does not halt ---

func TestRunner_SchemaViolation_DoesNotHaltFlow(t *testing.T) {
	// Step 1 returns a body with 'amount' as a string (schema violation)
	// but includes 'id' so the binding can still be extracted
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(201)
			// amount is a string — schema violation, but binding ($.id) still works
			w.Write([]byte(`{"id":"ord_123","amount":"bad","status":"pending"}`))
		} else {
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"ord_123","amount":100,"status":"pending","currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
	defer srv.Close()

	const flowWithSchema = `
flows:
  - id: schema-test
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
          body: {amount: 100, currency: GBP}
        expected:
          status_code: 201
          schema:
            type: object
            required: [id, amount, status]
            properties:
              id:     {type: string}
              amount: {type: integer}
              status: {type: string}
        extract:
          order_id: {from: "$.id", into: path}
      - id: retrieve
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{order_id}"
        expected:
          status_code: 200
`

	flow := mustLoadFlow(t, flowWithSchema)
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, _ := runner.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	t.Run("flow fails due to schema violation", func(t *testing.T) {
		if result.Passed {
			t.Error("expected flow to fail (schema violation on step 1)")
		}
	})
	t.Run("both steps attempted (schema violation does not halt)", func(t *testing.T) {
		if len(result.Steps) != 2 {
			t.Errorf("expected 2 step results, got %d", len(result.Steps))
		}
	})
	t.Run("step 1 has schema violations", func(t *testing.T) {
		if len(result.Steps[0].SchemaViolations) == 0 {
			t.Error("expected schema violations on step 1")
		}
	})
	t.Run("step 2 still executed", func(t *testing.T) {
		if result.Steps[1].StatusCode == 0 {
			t.Error("expected step 2 to have been executed (non-zero status code)")
		}
	})
}

// --- Execute: artifact written on failure ---

func TestRunner_Failure_WritesArtifact(t *testing.T) {
	srv := bindingMissingServer(t)
	defer srv.Close()

	dir := t.TempDir()
	flow := mustLoadFlow(t, createAndRetrieveYAML)
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL, ArtifactDir: dir})
	results, _ := runner.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	t.Run("artifact path is set on failed flow", func(t *testing.T) {
		if result.ArtifactPath == "" {
			t.Error("expected ArtifactPath to be set on failed flow")
		}
	})
	t.Run("artifact file exists on disk", func(t *testing.T) {
		if _, err := os.Stat(result.ArtifactPath); os.IsNotExist(err) {
			t.Errorf("artifact file does not exist at %q", result.ArtifactPath)
		}
	})
}

func TestRunner_Success_NoArtifactWritten(t *testing.T) {
	srv := twoStepServer(t)
	defer srv.Close()

	dir := t.TempDir()
	flow := mustLoadFlow(t, createAndRetrieveYAML)
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL, ArtifactDir: dir})
	results, _ := runner.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	t.Run("no artifact written on success", func(t *testing.T) {
		if result.ArtifactPath != "" {
			t.Errorf("expected no artifact on successful flow, got path %q", result.ArtifactPath)
		}
		entries, _ := os.ReadDir(dir)
		if len(entries) != 0 {
			t.Errorf("expected no files in artifact dir for successful flow, got %d", len(entries))
		}
	})
}

// --- Replay ---

func TestRunner_Replay_UsesSavedBindings(t *testing.T) {
	var requestPaths []string
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPaths = append(requestPaths, r.URL.Path)
		n := atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 || n == 3 { // original create and replay create
			// First run: returns ord_123; replay: returns ord_456
			// Replay must use ord_123 from artifact, not ord_456 from server
			id := "ord_123"
			if n == 3 {
				id = "ord_456"
			}
			w.WriteHeader(201)
			w.Write([]byte(`{"id":"` + id + `","status":"pending","amount":100,"currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		} else {
			w.WriteHeader(200)
			w.Write([]byte(`{"id":"ord_123","amount":100,"status":"pending","currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
	defer srv.Close()

	dir := t.TempDir()

	// First execution — fails on step 2 to force artifact creation
	const failOnRetrieve = `
flows:
  - id: replay-test
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
          body: {amount: 100, currency: GBP}
        expected: {status_code: 201}
        extract:
          order_id: {from: "$.id", into: path}
      - id: retrieve
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{order_id}"
        expected: {status_code: 201}  # wrong — server returns 200, so this fails
`
	flow := mustLoadFlow(t, failOnRetrieve)
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL, ArtifactDir: dir})
	results, _ := runner.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	if result.ArtifactPath == "" {
		t.Skip("no artifact produced — cannot test replay")
	}

	// Replay — server now returns ord_456 for the create step,
	// but replay must use ord_123 from the artifact
	replayResults, err := runner.Replay(context.Background(), result.ArtifactPath)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	t.Run("replay uses saved binding ord_123 not server's ord_456", func(t *testing.T) {
		if len(replayResults.Steps) < 2 {
			t.Fatalf("expected 2 steps in replay result, got %d", len(replayResults.Steps))
		}
		if replayResults.Steps[1].Request.Path != "/orders/ord_123" {
			t.Errorf("replay path: want '/orders/ord_123', got %q", replayResults.Steps[1].Request.Path)
		}
	})
}

// --- Multiple flows ---

func TestRunner_MultipleFlows_AllExecuted(t *testing.T) {
	srv := twoStepServer(t)
	defer srv.Close()

	flows := []model.Flow{
		mustLoadFlow(t, createAndRetrieveYAML),
		mustLoadFlow(t, createAndRetrieveYAML),
	}
	flows[1].ID = "create-and-retrieve-2" // distinct ID

	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, err := runner.Execute(context.Background(), flows, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 FlowResults, got %d", len(results))
	}
}
```

Run and confirm tests fail:

```bash
go test ./internal/strategy/stateful/... -run TestRunner -race -v
```

---

## Step 6 — MCP tool updates

### Spec

**`bt_run` tool:**
- Accepts `strategy: stateful` in its input
- Returns per-flow results: flow ID, passed/failed, step count, first failing step ID (if any), artifact path (if any)
- Returns a structured error if no flows directory is configured and no trace profile is present

**`bt_suggest_strategy` tool:**
- Already suggests `property`, `table`, `contract` based on operation characteristics
- New rule: when the loaded `TraceProfile` has a non-nil `Sequences` with at least two operations in `StartProbability`, suggest `stateful` with message: `"A trace profile with sequence data is present. The stateful strategy can generate multi-step flows from the observed operation sequences."`
- Stateful suggestion appears after property but before table in the suggestion list

### Tests

`internal/mcp/stateful_tools_test.go`:

```go
package mcp_test

import (
	"context"
	"strings"
	"testing"

	btmcp "github.com/yourorg/bt/internal/mcp"
	"github.com/yourorg/bt/pkg/model"
)

// --- bt_suggest_strategy ---

func TestSuggestStrategy_WithTraceProfile_SuggestsStateful(t *testing.T) {
	tool := btmcp.NewSuggestStrategyTool()

	profile := &model.TraceProfile{
		SchemaVersion: "1",
		Sequences: &model.SequenceProfile{
			StartProbability: map[string]float64{
				"CreateOrder": 0.85,
				"ListOrders":  0.15,
			},
			Transitions: map[string]map[string]float64{
				"CreateOrder": {"GetOrder": 0.87, "__END__": 0.13},
				"GetOrder":    {"__END__": 1.0},
				"ListOrders":  {"__END__": 1.0},
			},
		},
	}

	suggestions, err := tool.Suggest(context.Background(), btmcp.SuggestStrategyInput{
		TraceProfile: profile,
	})
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}

	names := strategyNames(suggestions)

	t.Run("stateful is suggested when trace profile has sequence data", func(t *testing.T) {
		if !containsStrategy(names, "stateful") {
			t.Errorf("expected 'stateful' in suggestions; got: %v", names)
		}
	})
}

func TestSuggestStrategy_WithTraceProfile_StatefulMentionsSequenceData(t *testing.T) {
	tool := btmcp.NewSuggestStrategyTool()
	profile := &model.TraceProfile{
		SchemaVersion: "1",
		Sequences: &model.SequenceProfile{
			StartProbability: map[string]float64{"CreateOrder": 1.0},
			Transitions:      map[string]map[string]float64{"CreateOrder": {"__END__": 1.0}},
		},
	}

	suggestions, _ := tool.Suggest(context.Background(), btmcp.SuggestStrategyInput{
		TraceProfile: profile,
	})

	for _, s := range suggestions {
		if s.Name == "stateful" {
			if !strings.Contains(strings.ToLower(s.Reason), "sequence") &&
				!strings.Contains(strings.ToLower(s.Reason), "flow") {
				t.Errorf("stateful suggestion reason should mention sequences or flows; got: %q", s.Reason)
			}
			return
		}
	}
	t.Error("stateful suggestion not found")
}

func TestSuggestStrategy_WithTraceProfile_StatefulAfterProperty(t *testing.T) {
	tool := btmcp.NewSuggestStrategyTool()
	profile := &model.TraceProfile{
		SchemaVersion: "1",
		Sequences: &model.SequenceProfile{
			StartProbability: map[string]float64{"CreateOrder": 0.85, "ListOrders": 0.15},
			Transitions:      map[string]map[string]float64{"CreateOrder": {"__END__": 1.0}, "ListOrders": {"__END__": 1.0}},
		},
	}

	suggestions, _ := tool.Suggest(context.Background(), btmcp.SuggestStrategyInput{
		TraceProfile: profile,
	})

	propertyIdx := -1
	statefulIdx := -1
	tableIdx := -1
	for i, s := range suggestions {
		switch s.Name {
		case "property":
			propertyIdx = i
		case "stateful":
			statefulIdx = i
		case "table":
			tableIdx = i
		}
	}

	t.Run("stateful appears after property", func(t *testing.T) {
		if propertyIdx == -1 || statefulIdx == -1 {
			t.Skip("property or stateful not in suggestions")
		}
		if statefulIdx <= propertyIdx {
			t.Errorf("stateful (index %d) should appear after property (index %d)", statefulIdx, propertyIdx)
		}
	})
	t.Run("stateful appears before table", func(t *testing.T) {
		if tableIdx == -1 || statefulIdx == -1 {
			t.Skip("table or stateful not in suggestions")
		}
		if statefulIdx >= tableIdx {
			t.Errorf("stateful (index %d) should appear before table (index %d)", statefulIdx, tableIdx)
		}
	})
}

func TestSuggestStrategy_NoTraceProfile_StatefulNotSuggested(t *testing.T) {
	tool := btmcp.NewSuggestStrategyTool()
	suggestions, _ := tool.Suggest(context.Background(), btmcp.SuggestStrategyInput{
		TraceProfile: nil,
	})
	names := strategyNames(suggestions)
	if containsStrategy(names, "stateful") {
		t.Error("stateful must not be suggested when no trace profile is present")
	}
}

func TestSuggestStrategy_TraceProfileWithoutSequences_StatefulNotSuggested(t *testing.T) {
	tool := btmcp.NewSuggestStrategyTool()
	profile := &model.TraceProfile{
		SchemaVersion: "1",
		Sequences:     nil, // no sequence data
	}
	suggestions, _ := tool.Suggest(context.Background(), btmcp.SuggestStrategyInput{
		TraceProfile: profile,
	})
	names := strategyNames(suggestions)
	if containsStrategy(names, "stateful") {
		t.Error("stateful must not be suggested when trace profile has no Sequences")
	}
}

func TestSuggestStrategy_TraceProfileWithOneOperation_StatefulNotSuggested(t *testing.T) {
	// Single operation in StartProbability — not enough for a meaningful flow
	tool := btmcp.NewSuggestStrategyTool()
	profile := &model.TraceProfile{
		SchemaVersion: "1",
		Sequences: &model.SequenceProfile{
			StartProbability: map[string]float64{"CreateOrder": 1.0},
			Transitions:      map[string]map[string]float64{"CreateOrder": {"__END__": 1.0}},
		},
	}
	suggestions, _ := tool.Suggest(context.Background(), btmcp.SuggestStrategyInput{
		TraceProfile: profile,
	})
	names := strategyNames(suggestions)
	if containsStrategy(names, "stateful") {
		t.Error("stateful must not be suggested for a profile with only one operation in StartProbability")
	}
}

// --- bt_run with strategy: stateful ---

func TestBtRun_StatefulStrategy_ReturnsPerFlowResults(t *testing.T) {
	tool := btmcp.NewBtRunTool()
	input := btmcp.BtRunInput{
		Strategy:  "stateful",
		FlowsDir:  "testdata/flows/",
		ConfigPath: "testdata/backendtest.yaml",
	}
	// This test exercises the MCP interface shape, not full execution.
	// The tool must accept strategy:"stateful" without returning an "unknown strategy" error.
	result, err := tool.ValidateInput(input)
	if err != nil {
		t.Fatalf("bt_run with strategy:stateful should be accepted: %v", err)
	}
	if result.Strategy != "stateful" {
		t.Errorf("expected Strategy='stateful', got %q", result.Strategy)
	}
}

func TestBtRun_StatefulStrategy_NoFlowsDir_ReturnsConfigError(t *testing.T) {
	tool := btmcp.NewBtRunTool()
	input := btmcp.BtRunInput{
		Strategy:  "stateful",
		FlowsDir:  "",  // no flows dir
		ConfigPath: "testdata/backendtest.yaml",
	}
	_, err := tool.ValidateInput(input)
	if err == nil {
		t.Error("expected error when strategy is stateful but no flows dir is configured")
	}
}

// --- helpers ---

type strategySuggestion struct {
	Name   string
	Reason string
}

func strategyNames(suggestions []strategySuggestion) []string {
	names := make([]string, len(suggestions))
	for i, s := range suggestions {
		names[i] = s.Name
	}
	return names
}

func containsStrategy(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}
```

Run and confirm tests fail:

```bash
go test ./internal/mcp/... -run "TestSuggestStrategy|TestBtRun_Stateful" -race -v
```

---

## Implementation

Only begin once all tests are written and confirmed failing.

> **Package layout is indicative.** The binding engine, loader, generator, and runner may be structured differently in the actual implementation — for example as sub-packages of `internal/strategy/stateful` or consolidated into fewer files. The exit criteria and test assertions are the binding constraint.

### Recommended build order

1. **`pkg/model/flow.go`** — types only, no logic. Verify model round-trip tests pass first.

2. **`internal/strategy/stateful/binding`** — `Extract`, `Inject`, `ValidateExpression`, error types. No HTTP, no YAML. Dependency: `github.com/PaesslerAG/jsonpath`.

3. **`internal/strategy/stateful/loader`** — YAML parsing using `gopkg.in/yaml.v3`. Two-pass validation calls `binding.ValidateExpression`. Dependency: binding package.

4. **`internal/strategy/stateful/gen`** — Markov sampling. Dependency: `pkg/model` only (no HTTP, no YAML).

5. **`internal/strategy/stateful` runner** — wires binding + loader + gen + HTTP executor. Artifact writing reuses the M4 artifact format extended with `flow_id`, `steps`, `bindings_at_each_step`. `Replay` reads the artifact and substitutes recorded bindings.

6. **MCP tool updates** — add `strategy: stateful` branch to `bt_run`; add trace-profile-with-sequences branch to `bt_suggest_strategy`.

---

## Full verification

```bash
go test ./pkg/model/... -run "TestFlowModel|TestFlowResultModel" -race -v
go test ./internal/strategy/stateful/binding/... -race -v
go test ./internal/strategy/stateful/loader/... -race -v
go test ./internal/strategy/stateful/gen/... -race -v
go test ./internal/strategy/stateful/... -run TestRunner -race -v
go test ./internal/mcp/... -run "TestSuggestStrategy|TestBtRun_Stateful" -race -v

# Full suite — must not regress
go test ./... -race

golangci-lint run ./...
CGO_ENABLED=0 go build ./cmd/bt
```

---

## M13 exit criterion

1. `Flow` and `FlowResult` models round-trip through JSON without data loss; `SchemaViolations` is always `[]` not `null`; `BindingFailure.ResponseBody` is preserved
2. `binding.Extract` handles JSONPath, `$`, `header.<name>`, and `status`; `ErrBindingNotFound` message names the failing expression
3. `binding.Inject` handles `path`, `query.<name>`, `header.<name>`, and `body` targets; type mismatch returns `ErrBindingTypeMismatch`; `$` with non-body target returns `ErrConfigError`
4. Flow loader catches all four config error types at load time (invalid JSONPath, undefined key, forward reference, `$` with non-body target) — before any HTTP request is made
5. `LoadFlowsDir` loads all `*.yaml`/`*.yml` files; ignores non-YAML files; halts on first config error
6. `GenerateFlows` produces `Count` flows starting from valid operations; no flow exceeds `MaxSteps`; flows are not all identical over 20+ draws; nil/empty profile returns `[]`
7. Stateful runner: binding failure halts flow and records `Critical` `BindingFailure` with expression and response body; status code failure does not halt; schema violation does not halt; binding values from earlier steps available to all later steps
8. Failed flow writes artifact; successful flow writes no artifact
9. Replay uses saved binding values, not re-extracted values from server
10. `bt_suggest_strategy` suggests `stateful` only when trace profile has sequence data with ≥2 operations; suggests it after `property` and before `table`
11. All tests pass with `-race`