//go:build integration

package main_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"testing"

	ordersapi "github.com/jimbery/bt/examples/orders-api"
	"github.com/jimbery/bt/internal/strategy/stateful"
	"github.com/jimbery/bt/internal/strategy/stateful/loader"
	"github.com/jimbery/bt/internal/testutil"
	"github.com/jimbery/bt/pkg/model"
)

func flowYAML(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join(testutil.RepoRoot(t), "examples/orders-api/bt/flows", name)
}

func newOrdersFlowServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(ordersapi.NewRouter())
}

func runFlowYAMLFile(t *testing.T, srv *httptest.Server, yamlName string) model.FlowResult {
	t.Helper()
	flow, err := loader.LoadFlowFile(flowYAML(t, yamlName))
	if err != nil {
		t.Fatalf("LoadFlowFile %q: %v", yamlName, err)
	}
	rnr := stateful.NewRunner(stateful.Config{BaseURL: srv.URL})
	results, err := rnr.Execute(context.Background(), []model.Flow{*flow}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	return results[0]
}
