//go:build integration

package main_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	ordersapi "github.com/jimbery/bt/examples/orders-api"
	"github.com/jimbery/bt/internal/strategy/stateful"
	"github.com/jimbery/bt/internal/strategy/stateful/gen"
	"github.com/jimbery/bt/internal/testutil"
	"github.com/jimbery/bt/pkg/model"
)

// TestTraceGeneratedFlow_RunsWithoutError verifies trace-derived skeleton flows
// execute without executor or binding errors (M13.5 / M13 exit criterion).
func TestTraceGeneratedFlow_RunsWithoutError(t *testing.T) {
	root := testutil.RepoRoot(t)
	profPath := filepath.Join(root, "examples/orders-api/bt/.bt/trace/profile.json")
	profile, err := model.ParseProfile(profPath)
	if err != nil {
		if model.IsErrProfileNotFound(err) {
			t.Skipf("trace profile not found — run bt trace import first: %v", err)
		}
		t.Fatalf("ParseProfile: %v", err)
	}
	if profile.Sequences == nil || len(profile.Sequences.StartProbability) == 0 {
		t.Skip("trace profile has insufficient sequence data for flow generation")
	}

	flows := gen.GenerateFlows(profile, nil, gen.GenerateFlowsConfig{
		Count:    5,
		MaxSteps: 3,
	})
	if len(flows) == 0 {
		t.Fatal("GenerateFlows returned 0 flows from profile with sequence data")
	}

	srv := httptest.NewServer(ordersapi.NewRouter())
	defer srv.Close()

	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})

	results, err := runner.Execute(context.Background(), flows, nil)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(results) != len(flows) {
		t.Errorf("expected %d results, got %d", len(flows), len(results))
	}
	for _, r := range results {
		for _, step := range r.Steps {
			if step.BindingFailure != nil {
				t.Errorf("trace-generated flow %q step %q had unexpected BindingFailure: %v",
					r.FlowID, step.StepID, step.BindingFailure)
			}
		}
	}
}

func TestTraceGeneratedFlow_StepOperationIDsMatchProfile(t *testing.T) {
	root := testutil.RepoRoot(t)
	profPath := filepath.Join(root, "examples/orders-api/bt/.bt/trace/profile.json")
	profile, err := model.ParseProfile(profPath)
	if err != nil {
		if model.IsErrProfileNotFound(err) {
			t.Skipf("trace profile not found: %v", err)
		}
		t.Fatalf("ParseProfile: %v", err)
	}
	if profile.Sequences == nil {
		t.Skip("no sequence data")
	}

	flows := gen.GenerateFlows(profile, nil, gen.GenerateFlowsConfig{Count: 20, MaxSteps: 5})
	for _, f := range flows {
		for _, step := range f.Steps {
			if _, ok := profile.Operations[step.OperationID]; !ok {
				t.Errorf("trace-generated flow %q step %q references operation %q not in profile",
					f.ID, step.ID, step.OperationID)
			}
		}
	}
}
