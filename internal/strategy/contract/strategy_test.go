package contract_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jimbery/bt/internal/runner"
	"github.com/jimbery/bt/internal/strategy"
	"github.com/jimbery/bt/internal/strategy/contract"
	"github.com/jimbery/bt/pkg/model"
)

func newContractMux(t *testing.T, handlers map[string]http.HandlerFunc) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	for path, h := range handlers {
		mux.HandleFunc(path, h)
	}
	return httptest.NewServer(mux)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func orderSchemaJSON() string {
	return `{
		"type": "object",
		"required": ["id", "amount", "currency", "status", "created_at"],
		"properties": {
			"id":          {"type": "string"},
			"amount":      {"type": "integer"},
			"currency":    {"type": "string"},
			"description": {"type": "string", "nullable": true},
			"status":      {"type": "string", "enum": ["pending", "confirmed", "shipped", "delivered", "cancelled"]},
			"created_at":  {"type": "string"}
		}
	}`
}

func orderOp(path string) model.Operation {
	var sch model.SchemaRef
	_ = json.Unmarshal([]byte(orderSchemaJSON()), &sch)
	return model.Operation{
		ID:        "GetOrder",
		Method:    "GET",
		Path:      path,
		Responses: []model.ResponseSpec{{StatusCode: 200, Schema: &sch}},
	}
}

func TestContractStrategy_OperationReturnsConformingResponse_Passes(t *testing.T) {
	srv := newContractMux(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"id":          "ord_1",
				"amount":      100,
				"currency":    "GBP",
				"description": nil,
				"status":      "pending",
				"created_at":  "2024-01-01T00:00:00Z",
			})
		},
	})
	defer srv.Close()

	st := contract.New()
	op := orderOp("/orders/probe")
	cases, err := st.Plan(context.Background(), strategy.Spec{Kind: strategy.KindContract, Operations: []string{op.ID}}, []model.Operation{op})
	if err != nil {
		t.Fatal(err)
	}
	exec := runner.New(runner.Config{BaseURL: srv.URL, Timeout: runner.DefaultTimeout})
	results, err := st.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || !results[0].Passed {
		t.Fatalf("expected pass, got %+v err=%v", results, err)
	}
	if results[0].ContractSchemaRef == "" {
		t.Error("expected ContractSchemaRef set")
	}
}

func TestContractStrategy_ResponseMissingRequiredField_Fails(t *testing.T) {
	srv := newContractMux(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{
				"id":       "ord_1",
				"amount":   100,
				"currency": "GBP",
			})
		},
	})
	defer srv.Close()

	st := contract.New()
	op := orderOp("/orders/probe")
	cases, _ := st.Plan(context.Background(), strategy.Spec{Kind: strategy.KindContract, Operations: []string{op.ID}}, []model.Operation{op})
	exec := runner.New(runner.Config{BaseURL: srv.URL, Timeout: runner.DefaultTimeout})
	results, _ := st.Execute(context.Background(), cases, exec)
	if results[0].Passed {
		t.Fatal("expected failure")
	}
	n := 0
	for _, f := range results[0].Failures {
		if f.Path == "status" || f.Path == "created_at" {
			n++
		}
	}
	if n < 2 {
		t.Errorf("expected failures for status and created_at, got %+v", results[0].Failures)
	}
}

func TestContractStrategy_ResponseContainsWrongType_Fails(t *testing.T) {
	srv := newContractMux(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
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

	st := contract.New()
	op := orderOp("/orders/probe")
	cases, _ := st.Plan(context.Background(), strategy.Spec{Kind: strategy.KindContract, Operations: []string{op.ID}}, []model.Operation{op})
	exec := runner.New(runner.Config{BaseURL: srv.URL, Timeout: runner.DefaultTimeout})
	results, _ := st.Execute(context.Background(), cases, exec)
	if results[0].Passed {
		t.Fatal("expected failure")
	}
	found := false
	for _, f := range results[0].Failures {
		if f.Path == "amount" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected amount path in failures: %+v", results[0].Failures)
	}
}

func TestContractStrategy_ContextCancelled_ReturnsError(t *testing.T) {
	srv := newContractMux(t, map[string]http.HandlerFunc{
		"/orders/probe": func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		},
	})
	defer srv.Close()

	st := contract.New()
	op := orderOp("/orders/probe")
	cases, _ := st.Plan(context.Background(), strategy.Spec{Kind: strategy.KindContract, Operations: []string{op.ID}}, []model.Operation{op})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	exec := runner.New(runner.Config{BaseURL: srv.URL, Timeout: runner.DefaultTimeout})
	_, err := st.Execute(ctx, cases, exec)
	if err == nil {
		t.Fatal("expected context error")
	}
}

func TestContractStrategy_MultipleOperations_AllResultsReturned(t *testing.T) {
	srv := newContractMux(t, map[string]http.HandlerFunc{
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

	var healthSchema model.SchemaRef
	_ = json.Unmarshal([]byte(`{"type":"object","required":["status"],"properties":{"status":{"type":"string"}}}`), &healthSchema)
	ops := []model.Operation{
		{ID: "GetHealth", Method: "GET", Path: "/health", Responses: []model.ResponseSpec{{StatusCode: 200, Schema: &healthSchema}}},
		orderOp("/orders/probe"),
	}
	st := contract.New()
	cases, err := st.Plan(context.Background(), strategy.Spec{
		Kind:       strategy.KindContract,
		Operations: []string{"GetHealth", "GetOrder"},
	}, ops)
	if err != nil {
		t.Fatal(err)
	}
	exec := runner.New(runner.Config{BaseURL: srv.URL, Timeout: runner.DefaultTimeout})
	results, err := st.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Passed {
			t.Errorf("result[%d] failed: %+v", i, r.Failures)
		}
	}
}
