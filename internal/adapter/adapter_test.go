package adapter_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jimbery/bt/internal/adapter"
	"github.com/jimbery/bt/pkg/model"
)

// fakeAdapter is a test double that records calls and returns canned values.
type fakeAdapter struct {
	name           string
	operations     []model.Operation
	discoverErr    error
	validateErr    error
	discoverCalled bool
	validateCalled bool
}

func (f *fakeAdapter) Name() string { return f.name }

func (f *fakeAdapter) Discover(ctx context.Context, target model.Target) ([]model.Operation, error) {
	f.discoverCalled = true
	return f.operations, f.discoverErr
}

func (f *fakeAdapter) Validate(ctx context.Context, target model.Target) error {
	f.validateCalled = true
	return f.validateErr
}

func TestAdapter_DiscoverReturnsOperations(t *testing.T) {
	a := &fakeAdapter{
		name: "openapi",
		operations: []model.Operation{
			{ID: "GetOrder", Method: "GET", Path: "/orders/{id}"},
			{ID: "CreateOrder", Method: "POST", Path: "/orders"},
		},
	}

	ops, err := a.Discover(context.Background(), model.Target{Name: "orders-api"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a.discoverCalled {
		t.Error("expected Discover to be called")
	}
	if len(ops) != 2 {
		t.Errorf("expected 2 operations, got %d", len(ops))
	}
}

func TestAdapter_DiscoverPropagatesError(t *testing.T) {
	a := &fakeAdapter{
		name:        "openapi",
		discoverErr: errors.New("schema file not found"),
	}

	_, err := a.Discover(context.Background(), model.Target{})
	if err == nil {
		t.Fatal("expected error from Discover")
	}
}

func TestAdapter_ValidateSuccess(t *testing.T) {
	a := &fakeAdapter{name: "openapi"}

	if err := a.Validate(context.Background(), model.Target{Name: "orders-api"}); err != nil {
		t.Errorf("expected Validate to succeed, got: %v", err)
	}
	if !a.validateCalled {
		t.Error("expected Validate to be called")
	}
}

func TestAdapter_ValidatePropagatesError(t *testing.T) {
	a := &fakeAdapter{
		name:        "openapi",
		validateErr: errors.New("schema parse error"),
	}

	if err := a.Validate(context.Background(), model.Target{}); err == nil {
		t.Fatal("expected error from Validate")
	}
}

func TestAdapter_NameIsNonEmpty(t *testing.T) {
	a := &fakeAdapter{name: "openapi"}
	if a.Name() == "" {
		t.Error("adapter Name must not be empty")
	}
}

var _ adapter.Adapter = (*fakeAdapter)(nil)
