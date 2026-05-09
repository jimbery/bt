package table_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/internal/strategy/table"
	"github.com/jayimbery/bt/pkg/model"
)

func TestTableStrategy_Execute_WritesArtifactOnFailure(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()

	s := table.NewWithOptions(table.Options{
		ArtifactWriter: replay.NewWriter(artifactDir),
		Environment:    "test",
	})

	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}

	cases := []model.Case{
		{
			ID:          "create-order-valid",
			OperationID: "CreateOrder",
			Input:       model.CaseInput{Method: "POST", Path: "/orders"},
			Expected:    &model.CaseExpectation{StatusCode: 201},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if results[0].Passed {
		t.Fatal("expected case to fail")
	}

	if results[0].ArtifactPath == "" {
		t.Error("expected ArtifactPath to be set on failed result")
	}

	if _, err := os.Stat(results[0].ArtifactPath); os.IsNotExist(err) {
		t.Errorf("expected artifact file at %s", results[0].ArtifactPath)
	}
}

func TestTableStrategy_Execute_NoArtifactOnPass(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()

	s := table.NewWithOptions(table.Options{
		ArtifactWriter: replay.NewWriter(artifactDir),
		Environment:    "test",
	})

	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 200},
	}

	cases := []model.Case{
		{
			ID:       "health-check",
			Input:    model.CaseInput{Method: "GET", Path: "/health"},
			Expected: &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if !results[0].Passed {
		t.Fatal("expected case to pass")
	}
	if results[0].ArtifactPath != "" {
		t.Errorf("expected no ArtifactPath on passing result, got %q", results[0].ArtifactPath)
	}

	entries, _ := os.ReadDir(artifactDir)
	if len(entries) != 0 {
		t.Errorf("expected no artifact files for passing case, got %d", len(entries))
	}
}

func TestTableStrategy_Execute_ArtifactContainsFailureDetail(t *testing.T) {
	t.Parallel()
	artifactDir := t.TempDir()

	s := table.NewWithOptions(table.Options{
		ArtifactWriter: replay.NewWriter(artifactDir),
		Environment:    "staging",
	})

	exec := &fakeExecutor{
		response: model.ResponseDetail{
			StatusCode: 404,
			Body:       []byte(`{"error":"not found"}`),
		},
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

	loader := replay.NewLoader()
	artifact, err := loader.Load(results[0].ArtifactPath)
	if err != nil {
		t.Fatalf("cannot load artifact: %v", err)
	}

	if artifact.CaseID != "get-order-200" {
		t.Errorf("CaseID: got %q, want get-order-200", artifact.CaseID)
	}
	if artifact.StrategyKind != "table" {
		t.Errorf("StrategyKind: got %q, want table", artifact.StrategyKind)
	}
	if artifact.Environment != "staging" {
		t.Errorf("Environment: got %q, want staging", artifact.Environment)
	}
	if artifact.Response.StatusCode != 404 {
		t.Errorf("Response.StatusCode: got %d, want 404", artifact.Response.StatusCode)
	}
	if len(artifact.Failures) == 0 {
		t.Error("expected failures in artifact")
	}

	if !filepath.IsAbs(results[0].ArtifactPath) {
		t.Error("expected absolute artifact path")
	}
}

func TestTableStrategy_Execute_WithoutWriter_DoesNotPanic(t *testing.T) {
	t.Parallel()
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}

	cases := []model.Case{
		{
			ID:       "case-without-writer",
			Input:    model.CaseInput{Method: "GET", Path: "/"},
			Expected: &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if results[0].ArtifactPath != "" {
		t.Errorf("expected empty ArtifactPath when no writer configured, got %q", results[0].ArtifactPath)
	}
}
