package property_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	graphqladapt "github.com/jayimbery/bt/internal/adapter/graphql"
	"github.com/jayimbery/bt/internal/runner"
	gqlrunner "github.com/jayimbery/bt/internal/runner/graphql"
	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/internal/strategy/property"
	"github.com/jayimbery/bt/pkg/model"
)

func TestPropertyStrategy_GraphQL_ResponseMatchesSchema_DetectsBrokenAmount(t *testing.T) {
	root := testRepoRootProperty(t)
	schemaPath := filepath.Join(root, "examples/graphql-api/schema.graphql")
	ops, err := graphqladapt.New().Discover(context.Background(), model.Target{SchemaPath: schemaPath})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	var createOp *model.Operation
	for i := range ops {
		if ops[i].ID == "createOrder" {
			createOp = &ops[i]
			break
		}
	}
	if createOp == nil {
		t.Fatal("createOrder operation not found in schema")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"createOrder":{"id":"ord-0001","amount":"one hundred","currency":"GBP","status":"PENDING","createdAt":"2020-01-01T00:00:00Z"}}}`))
	}))
	t.Cleanup(srv.Close)

	httpExec := runner.New(runner.Config{BaseURL: srv.URL})
	gqlExec := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	exec := runner.NewGQLRESTExecutor(httpExec, gqlExec)

	st := property.New()
	spec := strategy.Spec{
		Kind:       strategy.KindProperty,
		Operations: []string{createOp.ID},
		Invariants: []model.Invariant{{Name: model.InvariantResponseMatchesSchema}},
		Config:     map[string]any{"checks": 50, "seed": int64(42)},
	}
	cases, err := st.Plan(context.Background(), spec, []model.Operation{*createOp})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	results, err := st.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: %d", len(results))
	}
	if results[0].Passed {
		t.Fatalf("expected failure for broken amount type, failures=%v", results[0].Failures)
	}
	found := false
	for _, f := range results[0].Failures {
		if f.Invariant == model.InvariantResponseMatchesSchema && (containsSub(f.Path, "amount") || containsSub(f.Message, "amount")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected response_matches_schema failure mentioning amount, got %#v", results[0].Failures)
	}
}

func testRepoRootProperty(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(wd, "go.mod")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("go.mod not found")
		}
		wd = parent
	}
}

func containsSub(s, sub string) bool {
	return len(s) >= len(sub) && (sub == "" || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
