package stateful_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/internal/strategy/stateful"
	"github.com/jayimbery/bt/internal/strategy/stateful/loader"
	"github.com/jayimbery/bt/pkg/model"
)

func twoStepServer(t *testing.T) *httptest.Server {
	t.Helper()
	var count int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"ord_123","status":"pending","amount":100,"currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		} else {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"ord_123","amount":100,"currency":"GBP","status":"pending","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
}

func bindingMissingServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"status":"pending"}`))
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

func TestRunner_HappyPath_BothStepsPass(t *testing.T) {
	srv := twoStepServer(t)
	defer srv.Close()

	flow := mustLoadFlow(t, createAndRetrieveYAML)
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, err := rnr.Execute(context.Background(), []model.Flow{flow}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 FlowResult, got %d", len(results))
	}
	result := results[0]

	if !result.Passed {
		t.Errorf("expected flow to pass; steps: %v", result.Steps)
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 step results, got %d", len(result.Steps))
	}
	if result.Steps[0].Bindings["order_id"] != "ord_123" {
		t.Errorf("expected binding order_id='ord_123', got %v", result.Steps[0].Bindings["order_id"])
	}
	if result.Steps[1].Request.Path != "/orders/ord_123" {
		t.Errorf("expected path '/orders/ord_123', got %q", result.Steps[1].Request.Path)
	}
}

func TestRunner_BindingFromEarlierStep_UsedInLaterStep(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"id":"ord_999","status":"pending","amount":100,"currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		} else {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"ord_999","status":"confirmed","amount":100,"currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
	defer srv.Close()

	flow := mustLoadFlow(t, threeStepYAML)
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, _ := rnr.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	if result.Steps[1].Request.Path != "/orders/ord_999" {
		t.Errorf("patch path: want '/orders/ord_999', got %q", result.Steps[1].Request.Path)
	}
	if result.Steps[2].Request.Path != "/orders/ord_999" {
		t.Errorf("retrieve path: want '/orders/ord_999', got %q", result.Steps[2].Request.Path)
	}
}

func TestRunner_BindingFailure_HaltsFlow(t *testing.T) {
	srv := bindingMissingServer(t)
	defer srv.Close()

	flow := mustLoadFlow(t, createAndRetrieveYAML)
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, _ := rnr.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	if result.Passed {
		t.Error("expected flow to fail when binding extraction fails")
	}
	if len(result.Steps) != 1 {
		t.Errorf("expected 1 step result (halted after step 1), got %d", len(result.Steps))
	}
	if result.Steps[0].BindingFailure == nil {
		t.Fatal("expected BindingFailure on first step")
	}
	if result.Steps[0].BindingFailure.Expression != "$.id" {
		t.Errorf("Expression: want '$.id', got %q", result.Steps[0].BindingFailure.Expression)
	}
	if result.Steps[0].BindingFailure.Severity != "Critical" {
		t.Errorf("Severity: want 'Critical', got %q", result.Steps[0].BindingFailure.Severity)
	}
	if len(result.Steps[0].BindingFailure.ResponseBody) == 0 {
		t.Error("BindingFailure.ResponseBody must be non-empty for diagnosis")
	}
}

func TestRunner_StatusCodeFailure_DoesNotHaltFlow(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(500)
			_, _ = w.Write([]byte(`{"id":"ord_123","error":"something failed"}`))
		} else {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"ord_123","amount":100,"status":"pending","currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
	defer srv.Close()

	flow := mustLoadFlow(t, createAndRetrieveYAML)
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, _ := rnr.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	if result.Passed {
		t.Error("expected flow to fail (step 1 got 500)")
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 step results (status failure does not halt), got %d", len(result.Steps))
	}
	if result.Steps[0].Passed {
		t.Error("expected step 1 to fail (status 500 != 201)")
	}
	if !result.Steps[1].Passed {
		t.Error("expected step 2 to pass (status 200 == 200)")
	}
}

func TestRunner_SchemaViolation_DoesNotHaltFlow(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"ord_123","amount":"bad","status":"pending"}`))
		} else {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"ord_123","amount":100,"status":"pending","currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
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
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, _ := rnr.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	if result.Passed {
		t.Error("expected flow to fail (schema violation on step 1)")
	}
	if len(result.Steps) != 2 {
		t.Errorf("expected 2 step results, got %d", len(result.Steps))
	}
	if len(result.Steps[0].SchemaViolations) == 0 {
		t.Error("expected schema violations on step 1")
	}
	if result.Steps[1].StatusCode == 0 {
		t.Error("expected step 2 to have been executed (non-zero status code)")
	}
}

func TestRunner_Failure_WritesArtifact(t *testing.T) {
	srv := bindingMissingServer(t)
	defer srv.Close()

	dir := t.TempDir()
	flow := mustLoadFlow(t, createAndRetrieveYAML)
	w := replay.NewWriter(dir)
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL, ArtifactWriter: w})
	results, _ := rnr.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	if result.ArtifactPath == "" {
		t.Error("expected ArtifactPath to be set on failed flow")
	}
	if _, err := os.Stat(result.ArtifactPath); os.IsNotExist(err) {
		t.Errorf("artifact file does not exist at %q", result.ArtifactPath)
	}
}

func TestRunner_Success_NoArtifactWritten(t *testing.T) {
	srv := twoStepServer(t)
	defer srv.Close()

	dir := t.TempDir()
	flow := mustLoadFlow(t, createAndRetrieveYAML)
	w := replay.NewWriter(dir)
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL, ArtifactWriter: w})
	results, _ := rnr.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	if result.ArtifactPath != "" {
		t.Errorf("expected no artifact on successful flow, got path %q", result.ArtifactPath)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected no files in artifact dir for successful flow, got %d", len(entries))
	}
}

func TestRunner_Replay_UsesSavedBindings(t *testing.T) {
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&count, 1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 || n == 3 {
			id := "ord_123"
			if n == 3 {
				id = "ord_456"
			}
			w.WriteHeader(201)
			_, _ = w.Write([]byte(`{"id":"` + id + `","status":"pending","amount":100,"currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		} else {
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"id":"ord_123","amount":100,"status":"pending","currency":"GBP","created_at":"2024-01-15T10:00:00Z"}`))
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
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
        expected: {status_code: 201}
`
	flow := mustLoadFlow(t, failOnRetrieve)
	w := replay.NewWriter(dir)
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL, ArtifactWriter: w})
	results, _ := rnr.Execute(context.Background(), []model.Flow{flow}, nil)
	result := results[0]

	if result.ArtifactPath == "" {
		t.Fatal("no artifact produced — cannot test replay")
	}

	replayResults, err := rnr.Replay(context.Background(), result.ArtifactPath)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(replayResults.Steps) < 2 {
		t.Fatalf("expected 2 steps in replay result, got %d", len(replayResults.Steps))
	}
	if replayResults.Steps[1].Request.Path != "/orders/ord_123" {
		t.Errorf("replay path: want '/orders/ord_123', got %q", replayResults.Steps[1].Request.Path)
	}
}

func TestRunner_MultipleFlows_AllExecuted(t *testing.T) {
	srv := twoStepServer(t)
	defer srv.Close()

	flows := []model.Flow{
		mustLoadFlow(t, createAndRetrieveYAML),
		mustLoadFlow(t, createAndRetrieveYAML),
	}
	flows[1].ID = "create-and-retrieve-2"

	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, err := rnr.Execute(context.Background(), flows, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 FlowResults, got %d", len(results))
	}
}
