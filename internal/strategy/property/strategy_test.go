package property_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jimbery/bt/internal/strategy"
	"github.com/jimbery/bt/internal/strategy/property"
	"github.com/jimbery/bt/pkg/model"
)

func TestPropertyStrategy_No5xx_StableServer(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1","status":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	op := model.Operation{
		ID:     "GetPing",
		Method: "GET",
		Path:   "/ping",
		Responses: []model.ResponseSpec{{
			StatusCode: 200,
			Schema: &model.SchemaRef{
				Type: "object",
				Properties: map[string]*model.SchemaRef{
					"id":     {Type: "string"},
					"status": {Type: "string"},
				},
				Required: []string{"id", "status"},
			},
		}},
	}
	st := property.New()
	spec := strategy.Spec{
		Kind:       strategy.KindProperty,
		Operations: []string{op.ID},
		Invariants: []model.Invariant{{Name: model.InvariantNo5xx}},
		Config:     map[string]any{"checks": 15, "seed": int64(42)},
	}
	cases, err := st.Plan(context.Background(), spec, []model.Operation{op})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	exec := &httpExecutor{base: srv.URL}
	results, err := st.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: got %d want 1", len(results))
	}
	if !results[0].Passed {
		t.Fatalf("expected pass, failures=%v", results[0].Failures)
	}
}

func TestPropertyStrategy_No5xx_FailsOn500(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(srv.Close)

	op := model.Operation{
		ID:     "GetFail",
		Method: "GET",
		Path:   "/fail",
	}
	st := property.New()
	spec := strategy.Spec{
		Kind:       strategy.KindProperty,
		Operations: []string{op.ID},
		Invariants: []model.Invariant{{Name: model.InvariantNo5xx}},
		Config:     map[string]any{"checks": 8, "seed": int64(1)},
	}
	cases, err := st.Plan(context.Background(), spec, []model.Operation{op})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	exec := &httpExecutor{base: srv.URL}
	results, err := st.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if results[0].Passed {
		t.Fatal("expected failure on 500 endpoint")
	}
	found := false
	for _, f := range results[0].Failures {
		if f.Invariant == model.InvariantNo5xx {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected no_5xx failure, got %v", results[0].Failures)
	}
}

type httpExecutor struct {
	base string
}

func (e *httpExecutor) Run(ctx context.Context, in model.CaseInput) (model.ResponseDetail, error) {
	req, err := http.NewRequestWithContext(ctx, in.Method, e.base+in.Path, nil)
	if err != nil {
		return model.ResponseDetail{}, err
	}
	for k, v := range in.Headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return model.ResponseDetail{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.ResponseDetail{}, err
	}
	h := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			h[k] = v[0]
		}
	}
	return model.ResponseDetail{StatusCode: resp.StatusCode, Headers: h, Body: body}, nil
}
