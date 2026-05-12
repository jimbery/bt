//go:build integration

package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jayimbery/bt/internal/runner"
	gqlrunner "github.com/jayimbery/bt/internal/runner/graphql"
	"github.com/jayimbery/bt/internal/strategy/table"
	"github.com/jayimbery/bt/pkg/model"
)

func TestGQLSchemaViolationAcceptance(t *testing.T) {
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()

	caseYAML := `
cases:
  - id: gql-schema-violation-acceptance-test
    operation_id: createOrder
    input:
      method: POST
      path: /graphql
      gql_query: |
        mutation CreateOrder($input: CreateOrderInput!) {
          createOrder(input: $input) { id amount currency status createdAt }
        }
      gql_operation_name: CreateOrder
      gql_variables:
        input:
          amount: 100
          currency: GBP
    expected:
      status_code: 200
      gql_no_errors: true
      gql_data_schema:
        type: object
        required: [createOrder]
        properties:
          createOrder:
            type: object
            required: [id, amount, currency, status, createdAt]
            properties:
              id: { type: string }
              amount: { type: string }
              currency: { type: string }
              status: { type: string }
              createdAt: { type: string }
`
	cases, err := table.LoadCasesFromReader(strings.NewReader(caseYAML))
	if err != nil {
		t.Fatalf("LoadCasesFromReader: %v", err)
	}

	strat := table.New()
	httpExec := runner.New(runner.Config{BaseURL: srv.URL})
	gqlExec := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	exec := runner.NewGQLRESTExecutor(httpExec, gqlExec)
	results, err := strat.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	result := results[0]

	t.Run("case fails (schema violation present)", func(t *testing.T) {
		if result.Passed {
			t.Error("expected the acceptance test case to fail")
		}
	})

	wantField := "$.data.createOrder.amount"
	t.Run("violation is on createOrder.amount", func(t *testing.T) {
		found := false
		for _, v := range result.SchemaViolations {
			if v.Field == wantField {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected violation on %q; violations: %v", wantField, result.SchemaViolations)
		}
	})

	t.Run("violation reports expected type as 'string'", func(t *testing.T) {
		for _, v := range result.SchemaViolations {
			if v.Field == wantField && v.ExpectedType != "string" {
				t.Errorf("ExpectedType: want 'string', got %q", v.ExpectedType)
			}
		}
	})

	t.Run("violation severity is Critical", func(t *testing.T) {
		for _, v := range result.SchemaViolations {
			if v.Field == wantField && v.Severity != model.SeverityCritical {
				t.Errorf("Severity: want Critical, got %v", v.Severity)
			}
		}
	})
}
