//go:build integration

package main_test

import (
	"testing"
)

func TestFlow_CreateAndRetrieve_Passes(t *testing.T) {
	srv := newOrdersFlowServer(t)
	defer srv.Close()
	result := runFlowYAMLFile(t, srv, "create-and-retrieve.yaml")

	if !result.Passed {
		t.Errorf("expected flow to pass; steps: %v", result.Steps)
	}
	if len(result.Steps) == 0 {
		t.Fatal("no steps in result")
	}
	if result.Steps[0].Bindings["order_id"] == nil || result.Steps[0].Bindings["order_id"] == "" {
		t.Error("expected non-empty order_id binding after create step")
	}
	if len(result.Steps) < 2 {
		t.Fatal("expected at least 2 steps")
	}
	id, _ := result.Steps[0].Bindings["order_id"].(string)
	wantPath := "/orders/" + id
	if result.Steps[1].Request.Path != wantPath {
		t.Errorf("retrieve path: want %q, got %q", wantPath, result.Steps[1].Request.Path)
	}
	if len(result.Steps[1].SchemaViolations) > 0 {
		t.Errorf("unexpected schema violations on retrieve step: %v", result.Steps[1].SchemaViolations)
	}
}

func TestFlow_CreateAndUpdate_Passes(t *testing.T) {
	srv := newOrdersFlowServer(t)
	defer srv.Close()
	result := runFlowYAMLFile(t, srv, "create-and-update.yaml")

	if !result.Passed {
		t.Errorf("expected flow to pass; steps: %v", result.Steps)
	}
	if len(result.Steps) < 2 {
		t.Fatal("expected 2 steps")
	}
	if len(result.Steps[1].SchemaViolations) > 0 {
		t.Errorf("schema violations on update step: %v", result.Steps[1].SchemaViolations)
	}
}

func TestFlow_CreateAndCancel_Passes(t *testing.T) {
	srv := newOrdersFlowServer(t)
	defer srv.Close()
	result := runFlowYAMLFile(t, srv, "create-and-cancel.yaml")

	if !result.Passed {
		t.Errorf("expected flow to pass; steps: %v", result.Steps)
	}
	if len(result.Steps) < 2 {
		t.Fatal("expected 2 steps")
	}
	if len(result.Steps[1].SchemaViolations) > 0 {
		t.Errorf("schema violations on cancel step: %v", result.Steps[1].SchemaViolations)
	}
}
