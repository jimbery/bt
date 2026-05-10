package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jayimbery/bt/internal/ai"
	"github.com/jayimbery/bt/internal/mcp/tools"
)

const stubSuggestionsJSON = `[
	{"name":"no_5xx","rationale":"Any HTTP 5xx response from this operation indicates a server-side bug.","confidence":"high","invariant_type":"no_5xx"},
	{"name":"response_matches_schema","rationale":"The 201 response body must conform to the declared schema.","confidence":"high","invariant_type":"response_matches_schema"},
	{"name":"idempotency_key_prevents_duplicates","rationale":"POST operations should respect idempotency keys to prevent duplicate resource creation.","confidence":"medium","invariant_type":"idempotency"}
]`

func mustMarshalInput(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("cannot marshal: %v", err)
	}
	return b
}

func mustUnmarshalResult(t *testing.T, data json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("cannot unmarshal: %v\nraw: %s", err, data)
	}
}

func TestSuggestInvariants_NoAPIKey_UsesStub(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		OperationID string `json:"operation_id"`
		Provider    string `json:"provider"`
		Suggestions []struct {
			Name          string `json:"name"`
			Rationale     string `json:"rationale"`
			Confidence    string `json:"confidence"`
			InvariantType string `json:"invariant_type"`
		} `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)

	if resp.OperationID != "CreateOrder" {
		t.Errorf("expected OperationID=CreateOrder, got %q", resp.OperationID)
	}
	if resp.Provider == "" {
		t.Error("provider field must be set")
	}
	if len(resp.Suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}
}

func TestSuggestInvariants_Response_HasRequiredFields(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, _ := h(context.Background(), input)

	var resp map[string]any
	mustUnmarshalResult(t, result, &resp)

	for _, field := range []string{"operation_id", "provider", "suggestions"} {
		if _, ok := resp[field]; !ok {
			t.Errorf("response missing required field %q", field)
		}
	}
}

func TestSuggestInvariants_EachSuggestion_HasRequiredFields(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, _ := h(context.Background(), input)

	var resp struct {
		Suggestions []map[string]any `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)

	for i, s := range resp.Suggestions {
		for _, field := range []string{"name", "rationale", "confidence", "invariant_type"} {
			if v, ok := s[field]; !ok || v == "" {
				t.Errorf("suggestion[%d] missing or empty field %q", i, field)
			}
		}
	}
}

func TestSuggestInvariants_Confidence_IsValidEnum(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, _ := h(context.Background(), input)

	var resp struct {
		Suggestions []struct {
			Confidence string `json:"confidence"`
		} `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)

	valid := map[string]bool{"high": true, "medium": true, "low": true}
	for i, s := range resp.Suggestions {
		if !valid[s.Confidence] {
			t.Errorf("suggestion[%d] has invalid confidence %q (must be high/medium/low)", i, s.Confidence)
		}
	}
}

func TestSuggestInvariants_InvariantType_IsValidEnum(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, _ := h(context.Background(), input)

	var resp struct {
		Suggestions []struct {
			InvariantType string `json:"invariant_type"`
		} `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)

	valid := map[string]bool{
		"no_5xx":                  true,
		"response_matches_schema": true,
		"idempotency":             true,
		"custom":                  true,
	}
	for i, s := range resp.Suggestions {
		if !valid[s.InvariantType] {
			t.Errorf("suggestion[%d] has invalid invariant_type %q", i, s.InvariantType)
		}
	}
}

func TestSuggestInvariants_UnknownOperationID_ReturnsStructuredError(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "OperationThatDoesNotExist",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured error response, not Go error: %v", err)
	}
	var resp map[string]any
	mustUnmarshalResult(t, result, &resp)
	if resp["code"] != "OPERATION_NOT_FOUND" {
		t.Errorf("expected code OPERATION_NOT_FOUND, got %v", resp["code"])
	}
}

func TestSuggestInvariants_BadModelResponse_FallsBackToStub(t *testing.T) {
	garbageProvider := ai.NewStubProvider("this is not json at all and cannot be parsed")
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(garbageProvider)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("tool must not error on bad model response: %v", err)
	}
	var resp struct {
		Suggestions []any `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)
	if len(resp.Suggestions) == 0 {
		t.Error("expected fallback suggestions even when model returns garbage")
	}
}

func TestSuggestInvariants_MissingSchemaPath_ReturnsValidationError(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	h := tools.SuggestInvariantsHandler(stub)
	input := mustMarshalInput(t, map[string]any{
		"operation_id": "CreateOrder",
	})
	_, err := h(context.Background(), input)
	if err == nil {
		t.Error("expected error for missing schema_path")
	}
}

func TestSuggestStrategy_WithAIProvider_UsesAI(t *testing.T) {
	stubResponse := `[{
		"operation_id":"CreateOrder",
		"strategies":[
			{"strategy":"table","priority":"recommended","rationale":"The enum constraints on currency make deterministic table tests straightforward."},
			{"strategy":"property","priority":"recommended","rationale":"The amount minimum and currency enum are ideal for property-based boundary testing."}
		]
	}]`
	stub := ai.NewStubProvider(stubResponse)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestStrategyHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Provider        string `json:"provider"`
		Recommendations []struct {
			OperationID string `json:"operation_id"`
			Strategies  []struct {
				Strategy  string `json:"strategy"`
				Rationale string `json:"rationale"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshalResult(t, result, &resp)
	_ = schemaPath

	if resp.Provider == "" {
		t.Error("expected provider field to be set")
	}
	if len(resp.Recommendations) == 0 {
		t.Error("expected at least one recommendation")
	}
	for _, rec := range resp.Recommendations {
		for _, s := range rec.Strategies {
			if s.Rationale == "" {
				t.Errorf("strategy %q has empty rationale", s.Strategy)
			}
		}
	}
}

func TestSuggestStrategy_WithNilProvider_UsesRuleBasedFallback(t *testing.T) {
	h := tools.SuggestStrategyHandler(nil)
	input := mustMarshalInput(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Provider        string `json:"provider"`
		Recommendations []any  `json:"recommendations"`
	}
	mustUnmarshalResult(t, result, &resp)
	if resp.Provider != "rules" {
		t.Errorf("expected provider='rules' when no AI configured, got %q", resp.Provider)
	}
	if len(resp.Recommendations) == 0 {
		t.Error("expected recommendations from rule-based fallback")
	}
}

func TestSuggestStrategy_BadAIResponse_FallsBackToRules(t *testing.T) {
	garbage := ai.NewStubProvider("not valid json for strategy recommendations")
	h := tools.SuggestStrategyHandler(garbage)
	input := mustMarshalInput(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "GetHealth", "method": "GET", "has_body": false},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Recommendations []any `json:"recommendations"`
	}
	mustUnmarshalResult(t, result, &resp)
	if len(resp.Recommendations) == 0 {
		t.Error("expected rule-based fallback recommendations when AI returns garbage")
	}
}

func TestAllExpectedTools_IncludesSuggestInvariants(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	allTools := tools.AllWithProvider(stub)
	found := false
	for _, tool := range allTools {
		if tool.Name == "bt_suggest_invariants" {
			found = true
		}
	}
	if !found {
		t.Error("expected bt_suggest_invariants to be registered")
	}
}
