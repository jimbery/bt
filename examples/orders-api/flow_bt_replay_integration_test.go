//go:build integration

package main_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	ordersapi "github.com/jayimbery/bt/examples/orders-api"
	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/internal/strategy/stateful"
	"github.com/jayimbery/bt/internal/strategy/stateful/loader"
	"github.com/jayimbery/bt/pkg/model"
)

func TestReplay_UsesSavedBindings_NotFreshServerPath(t *testing.T) {
	srv := httptest.NewServer(ordersapi.NewRouter())
	defer srv.Close()

	const failOnRetrieve = `
flows:
  - id: replay-test
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          method: POST
          path: /orders
          headers:
            Content-Type: application/json
          body:
            amount: 100
            currency: GBP
        expected:
          status_code: 201
        extract:
          order_id:
            from: "$.id"
            into: path
      - id: retrieve
        operation_id: GetOrder
        input:
          method: GET
          path: "/orders/{order_id}"
        expected:
          status_code: 999
`

	flow, err := loader.LoadFlow(strings.NewReader(failOnRetrieve))
	if err != nil {
		t.Fatalf("LoadFlow: %v", err)
	}
	dir := t.TempDir()
	runner := stateful.NewRunner(stateful.Config{BaseURL: srv.URL, ArtifactWriter: replay.NewWriter(dir)})

	results, err := runner.Execute(context.Background(), []model.Flow{*flow}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	result := results[0]

	if result.ArtifactPath == "" {
		t.Fatal("expected artifact path for failed flow")
	}
	if len(result.Steps) < 2 {
		t.Fatalf("expected at least 2 steps, got %d", len(result.Steps))
	}

	firstID, _ := result.Steps[0].Bindings["order_id"].(string)
	if firstID == "" {
		t.Fatal("no order_id binding captured — cannot test replay")
	}

	replayResult, err := runner.Replay(context.Background(), result.ArtifactPath)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(replayResult.Steps) != len(result.Steps) {
		t.Errorf("original had %d steps, replay has %d", len(result.Steps), len(replayResult.Steps))
	}
	if len(replayResult.Steps) < 2 {
		t.Fatal("fewer than 2 steps in replay")
	}
	wantPath := "/orders/" + firstID
	if replayResult.Steps[1].Request.Path != wantPath {
		t.Errorf("replay step 2 path: want %q (saved binding), got %q",
			wantPath, replayResult.Steps[1].Request.Path)
	}
}
