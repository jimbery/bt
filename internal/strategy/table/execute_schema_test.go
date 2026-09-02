package table_test

import (
	"context"
	"testing"

	"github.com/jimbery/bt/internal/strategy/table"
	"github.com/jimbery/bt/pkg/model"
)

func executeTableCase(t *testing.T, resp model.ResponseDetail, c model.Case) model.Result {
	t.Helper()
	s := table.New()
	exec := &fakeExecutor{response: resp}
	results, err := s.Execute(context.Background(), []model.Case{c}, exec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results: %d", len(results))
	}
	return results[0]
}

func mustInlineOrderSchema(t *testing.T) *model.SchemaRef {
	t.Helper()
	return buildSchemaRef(t, orderSchemaJSON)
}

func TestExecute_SchemaValid_CasePasses(t *testing.T) {
	resp := model.ResponseDetail{
		StatusCode: 200,
		Body:       []byte(`{"id":"ord_1","amount":100,"currency":"GBP","status":"pending"}`),
	}
	c := model.Case{
		ID:          "get-order-schema-valid",
		OperationID: "GetOrder",
		Input:       model.CaseInput{Method: "GET", Path: "/orders/ord_1"},
		Expected: &model.CaseExpectation{
			StatusCode: 200,
			Schema:     mustInlineOrderSchema(t),
		},
	}
	result := executeTableCase(t, resp, c)
	if !result.Passed {
		t.Errorf("expected pass, violations=%v failures=%v", result.SchemaViolations, result.Failures)
	}
	if len(result.SchemaViolations) != 0 {
		t.Errorf("violations: %v", result.SchemaViolations)
	}
}

func TestExecute_SchemaMissingRequiredField_CaseFails(t *testing.T) {
	resp := model.ResponseDetail{
		StatusCode: 200,
		Body:       []byte(`{"id":"ord_1","amount":100,"currency":"GBP"}`),
	}
	c := model.Case{
		ID:          "get-order-missing-status",
		OperationID: "GetOrder",
		Input:       model.CaseInput{Method: "GET", Path: "/orders/ord_1"},
		Expected: &model.CaseExpectation{
			StatusCode: 200,
			Schema:     mustInlineOrderSchema(t),
		},
	}
	result := executeTableCase(t, resp, c)
	if result.Passed {
		t.Fatal("expected failure")
	}
	if len(result.SchemaViolations) == 0 {
		t.Fatal("expected SchemaViolations")
	}
	assertViolationField(t, result.SchemaViolations, "$.status")
}

func TestExecute_SchemaWrongType_ViolationReportsFieldAndTypes(t *testing.T) {
	resp := model.ResponseDetail{
		StatusCode: 200,
		Body:       []byte(`{"id":"ord_1","amount":"not-a-number","currency":"GBP","status":"pending"}`),
	}
	c := model.Case{
		ID:          "get-order-wrong-type",
		OperationID: "GetOrder",
		Input:       model.CaseInput{Method: "GET", Path: "/orders/ord_1"},
		Expected: &model.CaseExpectation{
			StatusCode: 200,
			Schema:     mustInlineOrderSchema(t),
		},
	}
	result := executeTableCase(t, resp, c)
	if result.Passed {
		t.Fatal("expected failure")
	}
	v := findViolation(t, result.SchemaViolations, "$.amount")
	if v.ExpectedType != "integer" || v.ActualType != "string" {
		t.Errorf("types: %+v", v)
	}
	if v.Message == "" || v.Message == "schema mismatch" {
		t.Errorf("message=%q", v.Message)
	}
}

func TestExecute_NoSchema_BackwardsCompatible_NeverFails(t *testing.T) {
	resp := model.ResponseDetail{
		StatusCode: 200,
		Body:       []byte(`{"completely":"unexpected","shape":true}`),
	}
	c := model.Case{
		ID:          "no-schema-backwards-compat",
		OperationID: "GetOrder",
		Input:       model.CaseInput{Method: "GET", Path: "/orders/ord_1"},
		Expected: &model.CaseExpectation{
			StatusCode: 200,
		},
	}
	result := executeTableCase(t, resp, c)
	if !result.Passed {
		t.Fatal("expected pass without schema")
	}
	if len(result.SchemaViolations) != 0 {
		t.Errorf("violations=%v", result.SchemaViolations)
	}
}

func TestExecute_StatusCodeFailAndSchemaFail_BothReported(t *testing.T) {
	resp := model.ResponseDetail{
		StatusCode: 500,
		Body:       []byte(`{"error":"internal","amount":"wrong-type"}`),
	}
	c := model.Case{
		ID:          "both-fail",
		OperationID: "GetOrder",
		Input:       model.CaseInput{Method: "GET", Path: "/orders/ord_1"},
		Expected: &model.CaseExpectation{
			StatusCode: 200,
			Schema:     mustInlineOrderSchema(t),
		},
	}
	result := executeTableCase(t, resp, c)
	if result.Passed {
		t.Fatal("expected failure")
	}
	if len(result.SchemaViolations) == 0 {
		t.Fatal("expected schema violations when status also fails")
	}
}

func TestExecute_GQLDataSchema_ValidatesDataField(t *testing.T) {
	resp := model.ResponseDetail{
		StatusCode: 200,
		Body:       []byte(`{"data":{"createOrder":{"status":"pending"}}}`),
	}
	gqlSchema := &model.SchemaRef{
		Properties: map[string]*model.SchemaRef{
			"createOrder": buildSchemaRef(t, `{
				"type": "object",
				"required": ["id", "status"],
				"properties": {
					"id":     { "type": "string" },
					"status": { "type": "string" }
				}
			}`),
		},
	}
	c := model.Case{
		ID:          "gql-missing-id",
		OperationID: "createOrder",
		Input: model.CaseInput{
			Method:   "POST",
			Path:     "/graphql",
			GQLQuery: `{ createOrder { id status } }`,
		},
		Expected: &model.CaseExpectation{
			StatusCode:    200,
			GQLDataSchema: gqlSchema,
		},
	}
	result := executeTableCase(t, resp, c)
	if result.Passed {
		t.Fatal("expected failure for missing id")
	}
	assertViolationField(t, result.SchemaViolations, "$.data.createOrder.id")
}
