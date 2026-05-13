package model_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jayimbery/bt/pkg/model"
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
					Method:  "POST",
					Path:    "/orders",
					Body:    []byte(`{"amount":100,"currency":"GBP"}`),
					Headers: http.Header{"Content-Type": []string{"application/json"}},
				},
				Response: model.StepResponse{
					StatusCode: 201,
					Body:       []byte(`{"id":"ord_123","status":"pending"}`),
					Headers:    http.Header{"Content-Type": []string{"application/json"}},
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
				SchemaViolations: []model.SchemaViolation{},
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
		raw := make(map[string]any)
		_ = json.Unmarshal(data, &raw)
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
