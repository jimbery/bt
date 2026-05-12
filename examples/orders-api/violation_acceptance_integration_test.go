//go:build integration

package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jayimbery/bt/internal/runner"
	"github.com/jayimbery/bt/internal/strategy/table"
	"github.com/jayimbery/bt/pkg/model"
)

// TestSchemaViolationAcceptance verifies the deliberately wrong schema case
// produces a violation report with the required detail.
func TestSchemaViolationAcceptance(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	defer srv.Close()

	caseYAML := `
cases:
  - id: schema-violation-acceptance-test
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
      schema:
        type: object
        required: [id, amount, currency, status, created_at]
        properties:
          id: { type: string }
          amount: { type: string }
          currency: { type: string }
          status: { type: string }
          created_at: { type: string }
`
	cases, err := table.LoadCasesFromReader(strings.NewReader(caseYAML))
	if err != nil {
		t.Fatalf("LoadCasesFromReader: %v", err)
	}

	strat := table.New()
	exec := runner.New(runner.Config{BaseURL: srv.URL})
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
			t.Error("expected the acceptance test case to fail; the schema has an intentional violation")
		}
	})

	t.Run("violation is on field $.amount", func(t *testing.T) {
		if !violationHasField(result.SchemaViolations, "$.amount") {
			t.Errorf("expected violation on '$.amount'; violations: %v", result.SchemaViolations)
		}
	})

	t.Run("violation reports expected type as 'string'", func(t *testing.T) {
		for _, v := range result.SchemaViolations {
			if v.Field == "$.amount" && v.ExpectedType != "string" {
				t.Errorf("ExpectedType: want 'string', got %q", v.ExpectedType)
			}
		}
	})

	t.Run("violation reports actual type — not just 'schema mismatch'", func(t *testing.T) {
		for _, v := range result.SchemaViolations {
			if v.Field == "$.amount" {
				if v.ActualType == "" || v.ActualType == "schema mismatch" {
					t.Errorf("ActualType must be a descriptive type name, got %q", v.ActualType)
				}
			}
		}
	})

	t.Run("violation severity is Critical", func(t *testing.T) {
		for _, v := range result.SchemaViolations {
			if v.Field == "$.amount" && v.Severity != model.SeverityCritical {
				t.Errorf("Severity: want Critical, got %v", v.Severity)
			}
		}
	})

	t.Run("violation message is not empty or generic", func(t *testing.T) {
		for _, v := range result.SchemaViolations {
			if v.Field == "$.amount" {
				if v.Message == "" || v.Message == "schema mismatch" {
					t.Errorf("Message must be descriptive, got %q", v.Message)
				}
			}
		}
	})
}

func violationHasField(vv []model.SchemaViolation, field string) bool {
	for _, v := range vv {
		if v.Field == field {
			return true
		}
	}
	return false
}
