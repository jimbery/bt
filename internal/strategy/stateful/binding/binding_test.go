package binding_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/strategy/stateful/binding"
	"github.com/jimbery/bt/pkg/model"
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
	bindings := map[string]any{"order_id": map[string]any{"nested": "object"}}
	_, err := binding.Inject(step, bindings)
	if !binding.IsErrBindingTypeMismatch(err) {
		t.Errorf("expected ErrBindingTypeMismatch for object injected into path, got %T: %v", err, err)
	}
}

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

func TestInject_Body_JSON_ReplacesBracePlaceholders(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{
			Method: "POST",
			Path:   "/graphql",
			Body: map[string]any{
				"query": "query Q($id: ID!) { order(id: $id) { id } }",
				"variables": map[string]any{
					"id": "{order_id}",
				},
			},
		},
	}
	bindings := map[string]any{"order_id": "ord_graph_1"}
	resolved, err := binding.Inject(step, bindings)
	if err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if resolved.Body == nil {
		t.Fatal("expected body")
	}
	if strings.Contains(string(resolved.Body), "{order_id}") {
		t.Errorf("placeholder should be replaced, got %s", resolved.Body)
	}
	if !strings.Contains(string(resolved.Body), "ord_graph_1") {
		t.Errorf("expected bound id in JSON body, got %s", resolved.Body)
	}
}

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

func TestExtract_DollarSign_WithPathInto_IsConfigError(t *testing.T) {
	step := &model.FlowStep{
		Input: model.StepInput{Path: "/orders/{order_body}"},
		Extract: map[string]model.ExtractSpec{
			"order_body": {From: "$", Into: "path"},
		},
	}
	_, err := binding.Inject(step, map[string]any{"order_body": map[string]any{}})
	if !binding.IsErrConfigError(err) {
		t.Errorf("expected ErrConfigError for $ with into:path, got %T: %v", err, err)
	}
}
