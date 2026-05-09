package table_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/internal/strategy/table"
	"github.com/jayimbery/bt/pkg/model"
)

type fakeExecutor struct {
	response model.ResponseDetail
	err      error
}

func (f *fakeExecutor) Run(_ context.Context, _ model.CaseInput) (model.ResponseDetail, error) {
	return f.response, f.err
}

func writeCaseFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cases.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const minimalCases = `
cases:
  - id: get-order-200
    operation_id: GetOrder
    input:
      method: GET
      path: /orders/1
    expected:
      status_code: 200
  - id: create-order-201
    operation_id: CreateOrder
    input:
      method: POST
      path: /orders
      headers:
        Content-Type: application/json
      body:
        amount: 100
    expected:
      status_code: 201
`

func TestTableStrategy_Plan_LoadsCases(t *testing.T) {
	t.Parallel()
	path := writeCaseFile(t, minimalCases)

	s := table.New()
	spec := strategy.Spec{
		Kind:   strategy.KindTable,
		Config: map[string]any{"file": path},
	}

	cases, err := s.Plan(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Plan returned unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Errorf("expected 2 cases, got %d", len(cases))
	}
}

func TestTableStrategy_Plan_CaseFields(t *testing.T) {
	t.Parallel()
	path := writeCaseFile(t, minimalCases)

	s := table.New()
	spec := strategy.Spec{
		Kind:   strategy.KindTable,
		Config: map[string]any{"file": path},
	}

	cases, err := s.Plan(context.Background(), spec, nil)
	if err != nil {
		t.Fatalf("Plan returned unexpected error: %v", err)
	}

	c := cases[0]
	if c.ID != "get-order-200" {
		t.Errorf("ID: got %q, want get-order-200", c.ID)
	}
	if c.OperationID != "GetOrder" {
		t.Errorf("OperationID: got %q, want GetOrder", c.OperationID)
	}
	if c.Input.Method != "GET" {
		t.Errorf("Input.Method: got %q, want GET", c.Input.Method)
	}
	if c.Input.Path != "/orders/1" {
		t.Errorf("Input.Path: got %q, want /orders/1", c.Input.Path)
	}
	if c.Expected == nil {
		t.Fatal("expected non-nil Expected")
	}
	if c.Expected.StatusCode != 200 {
		t.Errorf("Expected.StatusCode: got %d, want 200", c.Expected.StatusCode)
	}
}

func TestTableStrategy_Plan_MissingFile(t *testing.T) {
	t.Parallel()
	s := table.New()
	spec := strategy.Spec{
		Kind:   strategy.KindTable,
		Config: map[string]any{"file": "/nonexistent/cases.yaml"},
	}

	_, err := s.Plan(context.Background(), spec, nil)
	if err == nil {
		t.Fatal("expected error for missing case file")
	}
}

func TestTableStrategy_Plan_MissingFileConfig(t *testing.T) {
	t.Parallel()
	s := table.New()
	spec := strategy.Spec{
		Kind:   strategy.KindTable,
		Config: map[string]any{},
	}

	_, err := s.Plan(context.Background(), spec, nil)
	if err == nil {
		t.Fatal("expected error when file config key is missing")
	}
}

func TestTableStrategy_Execute_PassesOnStatusMatch(t *testing.T) {
	t.Parallel()
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 200},
	}

	cases := []model.Case{
		{
			ID:          "get-order-200",
			OperationID: "GetOrder",
			Input:       model.CaseInput{Method: "GET", Path: "/orders/1"},
			Expected:    &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected result to pass, failures: %v", results[0].Failures)
	}
}

func TestTableStrategy_Execute_FailsOnStatusMismatch(t *testing.T) {
	t.Parallel()
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}

	cases := []model.Case{
		{
			ID:          "get-order-200",
			OperationID: "GetOrder",
			Input:       model.CaseInput{Method: "GET", Path: "/orders/1"},
			Expected:    &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if results[0].Passed {
		t.Error("expected result to fail on status mismatch")
	}
	if len(results[0].Failures) == 0 {
		t.Error("expected at least one failure recorded")
	}
	if results[0].Failures[0].Invariant != "status_code" {
		t.Errorf("Invariant: got %q, want status_code", results[0].Failures[0].Invariant)
	}
}

func TestTableStrategy_Execute_PassesWithNoExpectation(t *testing.T) {
	t.Parallel()
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}

	cases := []model.Case{
		{
			ID:          "fire-and-forget",
			OperationID: "CreateOrder",
			Input:       model.CaseInput{Method: "POST", Path: "/orders"},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if !results[0].Passed {
		t.Error("expected case with no expectation to pass")
	}
}

func TestTableStrategy_Execute_RecordsRequestAndResponse(t *testing.T) {
	t.Parallel()
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{
			StatusCode: 200,
			Body:       []byte(`{"id":"ord-1"}`),
		},
	}

	cases := []model.Case{
		{
			ID:    "get-order",
			Input: model.CaseInput{Method: "GET", Path: "/orders/1"},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if results[0].Response.StatusCode != 200 {
		t.Errorf("Response.StatusCode: got %d, want 200", results[0].Response.StatusCode)
	}
}

func TestTableStrategy_Name(t *testing.T) {
	t.Parallel()
	s := table.New()
	if s.Name() != strategy.KindTable {
		t.Errorf("Name: got %q, want %q", s.Name(), strategy.KindTable)
	}
}
