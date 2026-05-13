package strategy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/pkg/model"
)

type fakeStrategy struct {
	name            strategy.Kind
	planCalled      bool
	execCalled      bool
	planErr         error
	execErr         error
	casesToReturn   []model.Case
	resultsToReturn []model.Result
}

func (f *fakeStrategy) Name() strategy.Kind { return f.name }

func (f *fakeStrategy) Plan(_ context.Context, _ strategy.Spec, _ []model.Operation) ([]model.Case, error) {
	f.planCalled = true
	return f.casesToReturn, f.planErr
}

func (f *fakeStrategy) Execute(_ context.Context, _ []model.Case, _ strategy.Executor) ([]model.Result, error) {
	f.execCalled = true
	return f.resultsToReturn, f.execErr
}

type fakeExecutor struct {
	response model.ResponseDetail
	err      error
}

func (f *fakeExecutor) Run(_ context.Context, _ model.CaseInput) (model.ResponseDetail, error) {
	return f.response, f.err
}

func TestStrategy_PlanAndExecuteSequence(t *testing.T) {
	t.Parallel()
	s := &fakeStrategy{
		name:            strategy.KindTable,
		casesToReturn:   []model.Case{{ID: "case-001", OperationID: "GetOrder"}},
		resultsToReturn: []model.Result{{CaseID: "case-001", Passed: true}},
	}
	exec := &fakeExecutor{response: model.ResponseDetail{StatusCode: 200}}

	ctx := context.Background()
	spec := strategy.Spec{Kind: strategy.KindTable}
	ops := []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/{id}"}}

	cases, err := s.Plan(ctx, spec, ops)
	if err != nil {
		t.Fatalf("Plan returned unexpected error: %v", err)
	}
	if !s.planCalled {
		t.Error("expected Plan to be called")
	}
	if len(cases) != 1 {
		t.Errorf("expected 1 case, got %d", len(cases))
	}

	results, err := s.Execute(ctx, cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !s.execCalled {
		t.Error("expected Execute to be called")
	}
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestStrategy_PlanError_DoesNotImplyExecuteCalled(t *testing.T) {
	t.Parallel()
	s := &fakeStrategy{
		name:    strategy.KindProperty,
		planErr: errors.New("schema parse failed"),
	}

	_, err := s.Plan(context.Background(), strategy.Spec{Kind: strategy.KindProperty}, nil)
	if err == nil {
		t.Fatal("expected Plan to return an error")
	}
	if s.execCalled {
		t.Error("Execute should not have been called")
	}
}

func TestStrategy_KindConstants_NonEmptyAndDistinct(t *testing.T) {
	t.Parallel()
	kinds := []strategy.Kind{
		strategy.KindTable,
		strategy.KindProperty,
		strategy.KindFuzz,
		strategy.KindContract,
		strategy.KindStateful,
		strategy.KindGraph,
	}

	seen := make(map[strategy.Kind]bool)
	for _, k := range kinds {
		if k == "" {
			t.Error("strategy kind must not be empty string")
		}
		if seen[k] {
			t.Errorf("duplicate strategy kind: %q", k)
		}
		seen[k] = true
	}
}

func TestExecutor_RunReturnsResponse(t *testing.T) {
	t.Parallel()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 201, Body: []byte(`{"id":"ord-1"}`)},
	}
	resp, err := exec.Run(context.Background(), model.CaseInput{Method: "POST", Path: "/orders"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 201 {
		t.Errorf("StatusCode: got %d, want 201", resp.StatusCode)
	}
}

func TestExecutor_RunPropagatesError(t *testing.T) {
	t.Parallel()
	exec := &fakeExecutor{err: errors.New("connection refused")}
	_, err := exec.Run(context.Background(), model.CaseInput{Method: "GET", Path: "/orders"})
	if err == nil {
		t.Fatal("expected error from Run")
	}
}
