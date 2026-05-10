package model_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jayimbery/bt/pkg/model"
)

func TestTarget_ZeroValue(t *testing.T) {
	t.Parallel()
	var target model.Target
	if target.Name != "" {
		t.Errorf("expected empty Name, got %q", target.Name)
	}
	if target.Auth.Type != "" {
		t.Errorf("expected empty Auth.Type, got %q", target.Auth.Type)
	}
}

func TestTarget_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	original := model.Target{
		Name:       "orders-api",
		BaseURL:    "https://staging.example.com",
		SchemaPath: "./openapi.yaml",
		Auth: model.AuthConfig{
			Type: "bearer",
			Env:  "ORDERS_API_TOKEN",
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.Target
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Name != original.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
	}
	if decoded.BaseURL != original.BaseURL {
		t.Errorf("BaseURL: got %q, want %q", decoded.BaseURL, original.BaseURL)
	}
	if decoded.Auth.Type != original.Auth.Type {
		t.Errorf("Auth.Type: got %q, want %q", decoded.Auth.Type, original.Auth.Type)
	}
}

func TestOperation_ZeroValue(t *testing.T) {
	t.Parallel()
	var op model.Operation
	if op.Method != "" {
		t.Errorf("expected empty Method, got %q", op.Method)
	}
	if op.Parameters != nil {
		t.Errorf("expected nil Parameters slice, got %v", op.Parameters)
	}
	if op.RequestBody != nil {
		t.Errorf("expected nil RequestBody, got non-nil")
	}
}

func TestOperation_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	original := model.Operation{
		ID:     "CreateOrder",
		Method: "POST",
		Path:   "/orders",
		Tags:   []string{"orders"},
		Parameters: []model.Parameter{
			{
				Name:     "X-Idempotency-Key",
				In:       "header",
				Required: true,
				Schema:   &model.SchemaRef{Type: "string"},
			},
		},
		RequestBody: &model.SchemaRef{
			Type: "object",
			Properties: map[string]*model.SchemaRef{
				"amount": {Type: "integer"},
			},
			Required: []string{"amount"},
		},
		Responses: []model.ResponseSpec{
			{StatusCode: 201, Schema: &model.SchemaRef{Type: "object"}},
			{StatusCode: 400, Schema: &model.SchemaRef{Type: "object"}},
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

	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Method != original.Method {
		t.Errorf("Method: got %q, want %q", decoded.Method, original.Method)
	}
	if len(decoded.Parameters) != len(original.Parameters) {
		t.Errorf("Parameters length: got %d, want %d", len(decoded.Parameters), len(original.Parameters))
	}
	if decoded.RequestBody == nil {
		t.Error("expected non-nil RequestBody after round-trip")
	}
	if len(decoded.Responses) != 2 {
		t.Errorf("Responses length: got %d, want 2", len(decoded.Responses))
	}
}

func TestSchemaRef_NilSafe(t *testing.T) {
	t.Parallel()
	var s *model.SchemaRef
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal nil SchemaRef failed: %v", err)
	}
	if string(data) != "null" {
		t.Errorf("expected null, got %s", data)
	}
}

func TestSchemaRef_Composition(t *testing.T) {
	t.Parallel()
	original := model.SchemaRef{
		OneOf: []*model.SchemaRef{
			{Type: "string"},
			{Type: "integer"},
		},
		AnyOf: []*model.SchemaRef{
			{Type: "object", Nullable: true},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.SchemaRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if len(decoded.OneOf) != 2 {
		t.Errorf("OneOf length: got %d, want 2", len(decoded.OneOf))
	}
	if len(decoded.AnyOf) != 1 {
		t.Errorf("AnyOf length: got %d, want 1", len(decoded.AnyOf))
	}
	if !decoded.AnyOf[0].Nullable {
		t.Error("expected AnyOf[0].Nullable to be true")
	}
}

func TestCase_ZeroValue(t *testing.T) {
	t.Parallel()
	var c model.Case
	if c.Expected != nil {
		t.Error("expected nil Expected on zero Case")
	}
	if c.Meta != nil {
		t.Error("expected nil Meta on zero Case")
	}
}

func TestCase_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	original := model.Case{
		ID:          "case-001",
		OperationID: "CreateOrder",
		Input: model.CaseInput{
			Method:  "POST",
			Path:    "/orders",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    map[string]any{"amount": 100},
		},
		Expected: &model.CaseExpectation{StatusCode: 201},
		Meta:     map[string]any{"source": "table"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.Case
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Expected == nil {
		t.Fatal("expected non-nil Expected after round-trip")
	}
	if decoded.Expected.StatusCode != 201 {
		t.Errorf("Expected.StatusCode: got %d, want 201", decoded.Expected.StatusCode)
	}
}

func TestResult_PassedWithNoFailures(t *testing.T) {
	t.Parallel()
	r := model.Result{
		CaseID:     "case-001",
		Passed:     true,
		StatusCode: 200,
		Duration:   42 * time.Millisecond,
	}
	if !r.Passed {
		t.Error("expected Passed to be true")
	}
	if len(r.Failures) != 0 {
		t.Errorf("expected no failures, got %d", len(r.Failures))
	}
}

func TestResult_FailedWithFailures(t *testing.T) {
	t.Parallel()
	r := model.Result{
		CaseID:     "case-002",
		Passed:     false,
		StatusCode: 500,
		Failures: []model.Failure{
			{
				Invariant: "no_5xx",
				Message:   "received status 500",
				Expected:  "status < 500",
				Actual:    500,
			},
		},
	}
	if r.Passed {
		t.Error("expected Passed to be false")
	}
	if len(r.Failures) != 1 {
		t.Errorf("expected 1 failure, got %d", len(r.Failures))
	}
	if r.Failures[0].Invariant != "no_5xx" {
		t.Errorf("Invariant: got %q, want %q", r.Failures[0].Invariant, "no_5xx")
	}
}

func TestResult_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	original := model.Result{
		CaseID:       "case-003",
		Passed:       false,
		StatusCode:   422,
		Duration:     10 * time.Millisecond,
		Failures:     []model.Failure{{Invariant: "response_matches_schema", Message: "field 'id' missing"}},
		Request:      model.RequestDetail{Method: "POST", URL: "https://staging.example.com/orders", Body: []byte(`{"amount":0}`)},
		Response:     model.ResponseDetail{StatusCode: 422, Body: []byte(`{"error":"invalid"}`)},
		StrategyKind: "property",
		Seed:         42,
		CasesRun:     100,
		ShrinkCount:  3,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.Result
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.CaseID != original.CaseID {
		t.Errorf("CaseID: got %q, want %q", decoded.CaseID, original.CaseID)
	}
	if decoded.Passed != original.Passed {
		t.Errorf("Passed: got %v, want %v", decoded.Passed, original.Passed)
	}
	if string(decoded.Request.Body) != string(original.Request.Body) {
		t.Errorf("Request.Body: got %s, want %s", decoded.Request.Body, original.Request.Body)
	}
	if decoded.Duration != original.Duration {
		t.Errorf("Duration: got %v, want %v", decoded.Duration, original.Duration)
	}
	if decoded.StrategyKind != original.StrategyKind {
		t.Errorf("StrategyKind: got %q, want %q", decoded.StrategyKind, original.StrategyKind)
	}
	if decoded.Seed != original.Seed {
		t.Errorf("Seed: got %d, want %d", decoded.Seed, original.Seed)
	}
	if decoded.CasesRun != original.CasesRun {
		t.Errorf("CasesRun: got %d, want %d", decoded.CasesRun, original.CasesRun)
	}
	if decoded.ShrinkCount != original.ShrinkCount {
		t.Errorf("ShrinkCount: got %d, want %d", decoded.ShrinkCount, original.ShrinkCount)
	}
}

func TestArtifact_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC().Truncate(time.Second)
	original := model.Artifact{
		ID:           "artifact-001",
		StrategyKind: "property",
		Seed:         12345,
		CaseID:       "case-042",
		OccurredAt:   now,
		Environment:  "staging",
		Failures:     []model.Failure{{Invariant: "no_5xx", Message: "received 503"}},
		ShrinkTrace:  []string{"step-1: reduced body", "step-2: removed field"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.Artifact
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
	if decoded.Seed != original.Seed {
		t.Errorf("Seed: got %d, want %d", decoded.Seed, original.Seed)
	}
	if !decoded.OccurredAt.Equal(original.OccurredAt) {
		t.Errorf("OccurredAt: got %v, want %v", decoded.OccurredAt, original.OccurredAt)
	}
	if len(decoded.ShrinkTrace) != 2 {
		t.Errorf("ShrinkTrace length: got %d, want 2", len(decoded.ShrinkTrace))
	}
}

func TestInvariant_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	original := model.Invariant{
		Name:   "no_5xx",
		Config: map[string]any{"exclude": []any{"503"}},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.Invariant
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.Name != original.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
	}
	if decoded.Config == nil {
		t.Error("expected non-nil Config after round-trip")
	}
}
