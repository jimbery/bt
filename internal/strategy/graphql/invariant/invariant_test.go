package invariant_test

import (
	"testing"

	gqlinvariant "github.com/jayimbery/bt/internal/strategy/graphql/invariant"
	"github.com/jayimbery/bt/pkg/model"
)

func respWith(body string) model.ResponseDetail {
	return model.ResponseDetail{StatusCode: 200, Body: []byte(body)}
}

func orderOp() model.Operation {
	return model.Operation{
		ID:      "order",
		GQLKind: model.GQLQuery,
		GQLSelectionSchema: &model.SchemaRef{
			Type:     "object",
			Required: []string{"id", "amount", "status"},
			Properties: map[string]*model.SchemaRef{
				"id":     {Type: "string", Nullable: false},
				"amount": {Type: "integer", Nullable: false},
				"status": {Type: "string", Nullable: false},
			},
		},
	}
}

func TestEvaluateNoGQLErrors_NoErrorsKey_Passes(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":{"id":"ord_1"}}`)}
	fs := gqlinvariant.EvaluateNoGQLErrors(gqlinvariant.NoGQLErrorsConfig{}, orderOp(), res)
	if len(fs) != 0 {
		t.Errorf("expected no failures when 'errors' key is absent, got: %v", fs)
	}
}

func TestEvaluateNoGQLErrors_ErrorsNull_Passes(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":{"id":"ord_1"},"errors":null}`)}
	fs := gqlinvariant.EvaluateNoGQLErrors(gqlinvariant.NoGQLErrorsConfig{}, orderOp(), res)
	if len(fs) != 0 {
		t.Errorf("expected no failures when 'errors' is null, got: %v", fs)
	}
}

func TestEvaluateNoGQLErrors_ErrorsEmptyArray_Passes(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":{"id":"ord_1"},"errors":[]}`)}
	fs := gqlinvariant.EvaluateNoGQLErrors(gqlinvariant.NoGQLErrorsConfig{}, orderOp(), res)
	if len(fs) != 0 {
		t.Errorf("expected no failures when 'errors' is empty array, got: %v", fs)
	}
}

func TestEvaluateNoGQLErrors_ErrorsNonEmpty_Fails(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":null,"errors":[{"message":"not found","locations":[]}]}`)}
	fs := gqlinvariant.EvaluateNoGQLErrors(gqlinvariant.NoGQLErrorsConfig{}, orderOp(), res)
	if len(fs) == 0 {
		t.Fatal("expected failure when 'errors' is a non-empty array")
	}
}

func TestEvaluateNoGQLErrors_ErrorsNonEmpty_IncludesFirstErrorMessage(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":null,"errors":[{"message":"resolver panicked"}]}`)}
	fs := gqlinvariant.EvaluateNoGQLErrors(gqlinvariant.NoGQLErrorsConfig{}, orderOp(), res)
	if len(fs) == 0 {
		t.Fatal("expected failure")
	}
	if !containsString(fs[0].Message, "resolver panicked") {
		t.Errorf("expected failure message to include 'resolver panicked', got: %q", fs[0].Message)
	}
}

func TestEvaluateNoGQLErrors_ErrorsNonEmpty_MessageAbsent_UsesGenericMessage(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":null,"errors":[{"locations":[]}]}`)}
	fs := gqlinvariant.EvaluateNoGQLErrors(gqlinvariant.NoGQLErrorsConfig{}, orderOp(), res)
	if len(fs) == 0 {
		t.Fatal("expected failure")
	}
	if fs[0].Message == "" {
		t.Error("failure message must not be empty even when errors[0].message is absent")
	}
}

func TestEvaluateNoGQLErrors_WarningClassification(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":null,"errors":[{"message":"oops"}]}`)}
	fs := gqlinvariant.EvaluateNoGQLErrors(gqlinvariant.NoGQLErrorsConfig{Severity: model.SeverityWarning}, orderOp(), res)
	if len(fs) == 0 {
		t.Fatal("expected failure")
	}
	if fs[0].Classification != "graphql_warning" {
		t.Errorf("expected graphql_warning classification, got %q", fs[0].Classification)
	}
}

func TestEvaluateNoGQLErrors_ReturnValueIsNeverNil(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":{"id":"ord_1"}}`)}
	fs := gqlinvariant.EvaluateNoGQLErrors(gqlinvariant.NoGQLErrorsConfig{}, orderOp(), res)
	if fs == nil {
		t.Error("Evaluate must return an empty slice, not nil")
	}
}

func TestEvaluateResponseMatchesSelection_ValidBody_NoFailures(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":{"order":{"id":"ord_1","amount":100,"status":"PENDING"}}}`)}
	op := orderOp()
	fs := gqlinvariant.EvaluateResponseMatchesSelection(op, res)
	if len(fs) != 0 {
		t.Errorf("expected no failures for valid body, got: %v", fs)
	}
}

func TestEvaluateResponseMatchesSelection_MissingRequiredField_Fails(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":{"order":{"id":"ord_1","status":"PENDING"}}}`)}
	op := orderOp()
	fs := gqlinvariant.EvaluateResponseMatchesSelection(op, res)
	if len(fs) == 0 {
		t.Fatal("expected failure for missing required field 'amount'")
	}
	found := false
	for _, f := range fs {
		if containsString(f.Path, "amount") || containsString(f.Message, "amount") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected failure mentioning 'amount'; got: %v", fs)
	}
}

func TestEvaluateResponseMatchesSelection_WrongType_Fails(t *testing.T) {
	res := model.Result{Response: respWith(`{"data":{"order":{"id":"ord_1","amount":"one hundred","status":"PENDING"}}}`)}
	op := orderOp()
	fs := gqlinvariant.EvaluateResponseMatchesSelection(op, res)
	if len(fs) == 0 {
		t.Fatal("expected failure for wrong type on 'amount'")
	}
}

func TestEvaluateResponseMatchesSelection_NoSelectionSchema_NoFailures(t *testing.T) {
	op := model.Operation{ID: "ping", GQLKind: model.GQLQuery, GQLDocument: "{}", GQLSelectionSchema: nil}
	res := model.Result{Response: respWith(`{"data":"pong"}`)}
	fs := gqlinvariant.EvaluateResponseMatchesSelection(op, res)
	if len(fs) != 0 {
		t.Errorf("expected no failures when no selection schema is set, got: %v", fs)
	}
}

func containsString(s, sub string) bool {
	if sub == "" {
		return true
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
