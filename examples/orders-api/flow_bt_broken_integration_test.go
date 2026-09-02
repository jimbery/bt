//go:build integration

package main_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	ordersapi "github.com/jimbery/bt/examples/orders-api"
	"github.com/jimbery/bt/internal/replay"
	"github.com/jimbery/bt/internal/strategy/stateful"
	"github.com/jimbery/bt/internal/strategy/stateful/loader"
	"github.com/jimbery/bt/pkg/model"
)

func TestBrokenFlow_WrongExpectedStatusOnStep2(t *testing.T) {
	srv := httptest.NewServer(ordersapi.NewRouter())
	defer srv.Close()

	const brokenFlowYAML = `
flows:
  - id: broken-retrieve-status
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
          headers:
            Content-Type: application/json
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
          status_code: 201
`

	flow, err := loader.LoadFlow(strings.NewReader(brokenFlowYAML))
	if err != nil {
		t.Fatalf("LoadFlow: %v", err)
	}

	dir := t.TempDir()
	rnr := stateful.NewRunner(stateful.Config{
		BaseURL:        srv.URL,
		ArtifactWriter: replay.NewWriter(dir),
	})
	results, err := rnr.Execute(context.Background(), []model.Flow{*flow}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result := results[0]

	if result.Passed {
		t.Error("expected flow to fail (step 2 has wrong expected status)")
	}
	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 step results, got %d", len(result.Steps))
	}
	if !result.Steps[0].Passed {
		t.Errorf("expected step 1 to pass; violations: %v, bindingFailure: %v",
			result.Steps[0].SchemaViolations, result.Steps[0].BindingFailure)
	}
	if result.Steps[1].Passed {
		t.Error("expected step 2 to fail (wrong expected status)")
	}
	if result.Steps[1].StatusCode != 200 {
		t.Errorf("step 2 actual status: want 200, got %d", result.Steps[1].StatusCode)
	}
	if !strings.Contains(result.Steps[1].Request.Path, "/orders/") {
		t.Errorf("step 2 path should contain '/orders/': got %q", result.Steps[1].Request.Path)
	}
	if result.ArtifactPath == "" {
		t.Fatal("expected artifact path to be set")
	}
	if _, err := os.Stat(result.ArtifactPath); os.IsNotExist(err) {
		t.Errorf("artifact file not found at %q", result.ArtifactPath)
	}
}

func TestBrokenFlow_ArtifactContainsPerStepDetail(t *testing.T) {
	srv := httptest.NewServer(ordersapi.NewRouter())
	defer srv.Close()

	const brokenFlowYAML = `
flows:
  - id: artifact-test
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
          headers:
            Content-Type: application/json
          body:
            amount: 100
            currency: GBP
        expected:
          status_code: 201
        extract:
          order_id: {from: "$.id", into: path}
      - id: retrieve
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{order_id}"
        expected:
          status_code: 999
`

	flow, err := loader.LoadFlow(strings.NewReader(brokenFlowYAML))
	if err != nil {
		t.Fatalf("LoadFlow: %v", err)
	}
	dir := t.TempDir()
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL, ArtifactWriter: replay.NewWriter(dir)})
	results, err := rnr.Execute(context.Background(), []model.Flow{*flow}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result := results[0]

	if result.ArtifactPath == "" {
		t.Skip("no artifact written — cannot verify artifact format")
	}

	data, err := os.ReadFile(result.ArtifactPath)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}

	var artifact map[string]any
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("artifact is not valid JSON: %v", err)
	}

	sr, ok := artifact["stateful_result"].(map[string]any)
	if !ok || sr == nil {
		t.Fatal("artifact missing stateful_result object")
	}
	if sr["flow_id"] == nil || sr["flow_id"] == "" {
		t.Error("stateful_result missing flow_id")
	}
	steps, ok := sr["steps"].([]any)
	if !ok || len(steps) == 0 {
		t.Fatal("stateful_result.steps must be a non-empty array")
	}
	step0, _ := steps[0].(map[string]any)
	if step0["step_id"] == nil {
		t.Error("step 0 missing step_id")
	}
	if _, ok := step0["bindings"]; !ok {
		t.Error("step 0 missing bindings key")
	}
	if step0["request"] == nil {
		t.Error("step 0 missing request")
	}
	if step0["response"] == nil {
		t.Error("step 0 missing response")
	}
	if len(steps) >= 2 {
		step1, _ := steps[1].(map[string]any)
		req, _ := step1["request"].(map[string]any)
		path, _ := req["path"].(string)
		if !strings.Contains(path, "/orders/") {
			t.Errorf("step 2 request path should contain injected id: got %q", path)
		}
	}
}
