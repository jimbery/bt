//go:build integration

package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	graphqladapt "github.com/jayimbery/bt/internal/adapter/graphql"
	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/internal/runner"
	gqlrunner "github.com/jayimbery/bt/internal/runner/graphql"
	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/internal/strategy/property"
	"github.com/jayimbery/bt/pkg/model"
)

func testRepoRootM115(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for i := 0; i < 16; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	t.Fatal("go.mod not found")
	panic("unreachable")
}

func discoveredGraphQLOpsM115(t *testing.T) []model.Operation {
	t.Helper()
	schemaPath := filepath.Join(testRepoRootM115(t), "examples/graphql-api/schema.graphql")
	ops, err := graphqladapt.New().Discover(context.Background(), model.Target{SchemaPath: schemaPath})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	return ops
}

func pickOpM115(t *testing.T, ops []model.Operation, id string) model.Operation {
	t.Helper()
	for i := range ops {
		if ops[i].ID == id {
			return ops[i]
		}
	}
	t.Fatalf("operation %q not found", id)
	panic("unreachable")
}

func newGQLTestServerM115(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(NewHandler())
}

func newBuggyGQLTestServerM115(t *testing.T) *httptest.Server {
	t.Helper()
	t.Setenv("BT_GQL_AMOUNT_BUG", "1")
	return httptest.NewServer(NewHandler())
}

func TestPropertyRun_ValidServer_AllPassWithin100Checks(t *testing.T) {
	srv := newGQLTestServerM115(t)
	defer srv.Close()

	ops := discoveredGraphQLOpsM115(t)
	createOp := pickOpM115(t, ops, "createOrder")
	orderOp := pickOpM115(t, ops, "order")
	selected := []model.Operation{createOp, orderOp}

	httpExec := runner.New(runner.Config{BaseURL: srv.URL})
	gqlExec := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	exec := runner.NewGQLRESTExecutor(httpExec, gqlExec)

	st := property.NewWithOptions(property.Options{
		ArtifactWriter: replay.NewWriter(filepath.Join(t.TempDir(), "artifacts")),
	})
	spec := strategy.Spec{
		Kind:       strategy.KindProperty,
		Operations: []string{"createOrder", "order"},
		Invariants: []model.Invariant{
			{Name: model.InvariantNoGQLErrors},
			{Name: model.InvariantResponseMatchesSchema},
		},
		Config: map[string]any{"checks": 100, "seed": int64(44)},
	}
	cases, err := st.Plan(context.Background(), spec, selected)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	results, err := st.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, r := range results {
		if !r.Passed {
			t.Errorf("case %q unexpectedly failed; failures: %v", r.CaseID, r.Failures)
		}
	}
}

func TestPropertyRun_BrokenResolver_DetectedWithin50Checks(t *testing.T) {
	srv := newBuggyGQLTestServerM115(t)
	defer srv.Close()

	ops := discoveredGraphQLOpsM115(t)
	createOp := pickOpM115(t, ops, "createOrder")

	httpExec := runner.New(runner.Config{BaseURL: srv.URL})
	gqlExec := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	exec := runner.NewGQLRESTExecutor(httpExec, gqlExec)

	st := property.NewWithOptions(property.Options{
		ArtifactWriter: replay.NewWriter(filepath.Join(t.TempDir(), "artifacts")),
	})
	spec := strategy.Spec{
		Kind:       strategy.KindProperty,
		Operations: []string{createOp.ID},
		Invariants: []model.Invariant{{Name: model.InvariantResponseMatchesSchema}},
		Config:     map[string]any{"checks": 50, "seed": int64(42)},
	}
	cases, err := st.Plan(context.Background(), spec, []model.Operation{createOp})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	results, err := st.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	anyFailed := false
	for _, r := range results {
		if !r.Passed {
			anyFailed = true
		}
	}
	if !anyFailed {
		t.Fatal("expected broken resolver to be detected within 50 checks; all cases passed")
	}

	for _, r := range results {
		if !r.Passed {
			mentionsAmount := false
			for _, f := range r.Failures {
				if containsSubstringM115(f.Path, "amount") || containsSubstringM115(f.Message, "amount") {
					mentionsAmount = true
					break
				}
			}
			if !mentionsAmount {
				t.Errorf("expected failure to mention 'amount'; failures: %v", r.Failures)
			}
		}
	}
}

func TestPropertyRun_ArtifactBundle_ContainsGQLFields(t *testing.T) {
	srv := newBuggyGQLTestServerM115(t)
	defer srv.Close()

	artifactDir := filepath.Join(t.TempDir(), "artifacts")
	ops := discoveredGraphQLOpsM115(t)
	createOp := pickOpM115(t, ops, "createOrder")

	httpExec := runner.New(runner.Config{BaseURL: srv.URL})
	gqlExec := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	exec := runner.NewGQLRESTExecutor(httpExec, gqlExec)

	st := property.NewWithOptions(property.Options{
		ArtifactWriter: replay.NewWriter(artifactDir),
	})
	spec := strategy.Spec{
		Kind:       strategy.KindProperty,
		Operations: []string{createOp.ID},
		Invariants: []model.Invariant{{Name: model.InvariantResponseMatchesSchema}},
		Config:     map[string]any{"checks": 50, "seed": int64(7)},
	}
	cases, _ := st.Plan(context.Background(), spec, []model.Operation{createOp})
	results, _ := st.Execute(context.Background(), cases, exec)

	for _, r := range results {
		if !r.Passed && r.ArtifactPath != "" {
			data, err := os.ReadFile(r.ArtifactPath)
			if err != nil {
				t.Fatalf("read artifact: %v", err)
			}
			var bundle map[string]any
			if err := json.Unmarshal(data, &bundle); err != nil {
				t.Fatalf("parse artifact JSON: %v", err)
			}
			if bundle["gql_operation_kind"] == nil {
				t.Errorf("artifact missing 'gql_operation_kind'; keys: %v", mapKeysM115(bundle))
			}
			if bundle["gql_variables"] == nil {
				t.Errorf("artifact missing 'gql_variables'; keys: %v", mapKeysM115(bundle))
			}
			if bundle["strategy_kind"] != "property" {
				t.Errorf("expected strategy_kind 'property', got: %v", bundle["strategy_kind"])
			}
			failures, ok := bundle["failures"].([]any)
			if !ok || len(failures) == 0 {
				t.Errorf("expected non-empty failures array in artifact; got: %v", bundle["failures"])
			}
			return
		}
	}
	t.Fatal("expected a failing result with artifact")
}

func TestPropertyRun_SeedReplay_ArtifactReproducible(t *testing.T) {
	srv := newBuggyGQLTestServerM115(t)
	defer srv.Close()

	dir := filepath.Join(t.TempDir(), "artifacts")
	seed := int64(99)

	runWithSeed := func() []model.Result {
		ops := discoveredGraphQLOpsM115(t)
		createOp := pickOpM115(t, ops, "createOrder")
		httpExec := runner.New(runner.Config{BaseURL: srv.URL})
		gqlExec := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
		exec := runner.NewGQLRESTExecutor(httpExec, gqlExec)
		st := property.NewWithOptions(property.Options{
			ArtifactWriter: replay.NewWriter(dir),
		})
		spec := strategy.Spec{
			Kind:       strategy.KindProperty,
			Operations: []string{createOp.ID},
			Invariants: []model.Invariant{{Name: model.InvariantResponseMatchesSchema}},
			Config:     map[string]any{"checks": 50, "seed": seed},
		}
		cases, _ := st.Plan(context.Background(), spec, []model.Operation{createOp})
		results, _ := st.Execute(context.Background(), cases, exec)
		return results
	}

	results1 := runWithSeed()
	results2 := runWithSeed()

	if len(results1) != len(results2) {
		t.Fatalf("different result counts: %d vs %d", len(results1), len(results2))
	}
	for i := range results1 {
		if results1[i].Passed != results2[i].Passed {
			t.Errorf("result[%d]: seeded runs disagree on pass/fail", i)
		}
	}

	for _, r := range results1 {
		if !r.Passed && r.ArtifactPath != "" {
			data, err := os.ReadFile(r.ArtifactPath)
			if err != nil {
				t.Fatalf("read artifact: %v", err)
			}
			var bundle map[string]any
			if err := json.Unmarshal(data, &bundle); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if bundle["seed"] == nil {
				t.Errorf("artifact missing 'seed' field; keys: %v", mapKeysM115(bundle))
			}
			return
		}
	}
	t.Fatal("expected a failing artifact with seed")
}

func containsSubstringM115(s, sub string) bool {
	if sub == "" {
		return true
	}
	if s == "" {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func mapKeysM115(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
