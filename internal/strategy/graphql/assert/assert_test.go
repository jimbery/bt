package assert_test

import (
	"testing"

	gqlassert "github.com/jayimbery/bt/internal/strategy/graphql/assert"
	"github.com/jayimbery/bt/pkg/model"
)

func productOp() model.Operation {
	return model.Operation{
		ID:      "product",
		GQLKind: model.GQLQuery,
		GQLSelectionSchema: &model.SchemaRef{
			Type: "object",
			Properties: map[string]*model.SchemaRef{
				"id":    {Type: "string", Nullable: false},
				"name":  {Type: "string", Nullable: false},
				"price": {Type: "number", Nullable: false},
			},
			Required: []string{"id", "name", "price"},
		},
	}
}

func TestAssertResponse_ValidDataNoErrors_NoFailures(t *testing.T) {
	t.Parallel()
	body := []byte(`{"data":{"id":"prod-1","name":"Widget","price":9.99}}`)
	failures := gqlassert.AssertResponse(body, productOp())
	if len(failures) != 0 {
		t.Errorf("expected no failures, got %d: %v", len(failures), failures)
	}
}

func TestAssertResponse_NotJSONObject_CriticalFailure(t *testing.T) {
	t.Parallel()
	body := []byte(`not json`)
	failures := gqlassert.AssertResponse(body, productOp())
	if len(failures) == 0 {
		t.Fatal("expected critical failure for non-JSON body")
	}
	if failures[0].Severity != gqlassert.Critical {
		t.Errorf("expected Critical, got %v", failures[0].Severity)
	}
}

func TestAssertResponse_MissingDataKey_CriticalFailure(t *testing.T) {
	t.Parallel()
	body := []byte(`{"errors":[{"message":"something went wrong"}]}`)
	failures := gqlassert.AssertResponse(body, productOp())
	hasCritical := false
	for _, f := range failures {
		if f.Severity == gqlassert.Critical {
			hasCritical = true
		}
	}
	if !hasCritical {
		t.Error("expected Critical failure when 'data' key is absent")
	}
}

func TestAssertResponse_ErrorsPresentWithData_WarningNotCritical(t *testing.T) {
	t.Parallel()
	body := []byte(`{
		"data": {"id":"prod-1","name":"Widget","price":9.99},
		"errors": [{"message":"non-critical warning from resolver"}]
	}`)
	failures := gqlassert.AssertResponse(body, productOp())
	for _, f := range failures {
		if f.Severity == gqlassert.Critical {
			t.Errorf("partial data with errors should not produce Critical failure, got: %v", f)
		}
	}
	hasWarning := false
	for _, f := range failures {
		if f.Severity == gqlassert.Warning {
			hasWarning = true
		}
	}
	if !hasWarning {
		t.Error("expected Warning for errors array presence")
	}
}

func TestAssertResponse_NullDataWithErrors_CriticalFailure(t *testing.T) {
	t.Parallel()
	body := []byte(`{"data":null,"errors":[{"message":"product not found"}]}`)
	failures := gqlassert.AssertResponse(body, productOp())
	hasCritical := false
	for _, f := range failures {
		if f.Severity == gqlassert.Critical {
			hasCritical = true
		}
	}
	if !hasCritical {
		t.Error("expected Critical failure when data is null and errors are present")
	}
}

func TestAssertResponse_ErrorItemMissingMessageField_CriticalFailure(t *testing.T) {
	t.Parallel()
	body := []byte(`{"data":null,"errors":[{"code":"NOT_FOUND"}]}`)
	failures := gqlassert.AssertResponse(body, productOp())
	hasCritical := false
	for _, f := range failures {
		if f.Field == "errors[0].message" && f.Severity == gqlassert.Critical {
			hasCritical = true
		}
	}
	if !hasCritical {
		t.Errorf("expected Critical failure on errors[0].message, failures: %v", failures)
	}
}

func TestAssertResponse_DataMissingRequiredField_CriticalFailure(t *testing.T) {
	t.Parallel()
	body := []byte(`{"data":{"id":"prod-1","name":"Widget"}}`)
	failures := gqlassert.AssertResponse(body, productOp())
	found := false
	for _, f := range failures {
		if f.Field == "data.price" && f.Severity == gqlassert.Critical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Critical failure on data.price, failures: %v", failures)
	}
}

func TestAssertResponse_DataFieldWrongType_CriticalFailure(t *testing.T) {
	t.Parallel()
	body := []byte(`{"data":{"id":"prod-1","name":"Widget","price":"expensive"}}`)
	failures := gqlassert.AssertResponse(body, productOp())
	found := false
	for _, f := range failures {
		if f.Field == "data.price" && f.Severity == gqlassert.Critical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Critical failure on data.price type mismatch, failures: %v", failures)
	}
}

func TestAssertResponse_NonNullFieldIsNull_CriticalFailure(t *testing.T) {
	t.Parallel()
	body := []byte(`{"data":{"id":"prod-1","name":null,"price":9.99}}`)
	failures := gqlassert.AssertResponse(body, productOp())
	found := false
	for _, f := range failures {
		if f.Field == "data.name" && f.Severity == gqlassert.Critical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Critical failure on data.name null violation, failures: %v", failures)
	}
}

func TestAssertResponse_NoSelectionSchema_OnlyEnvelopeChecked(t *testing.T) {
	t.Parallel()
	op := model.Operation{
		ID:                 "ping",
		GQLKind:            model.GQLQuery,
		GQLSelectionSchema: nil,
	}
	body := []byte(`{"data":"pong"}`)
	failures := gqlassert.AssertResponse(body, op)
	if len(failures) != 0 {
		t.Errorf("expected no failures when no selection schema is set, got: %v", failures)
	}
}

func TestAssertResponse_nullSelectionSkipsSchemaCheck(t *testing.T) {
	t.Parallel()
	op := model.Operation{
		ID:      "widget",
		GQLKind: model.GQLQuery,
		GQLSelectionSchema: &model.SchemaRef{
			Type: "object",
			Properties: map[string]*model.SchemaRef{
				"id": {Type: "string", Nullable: false},
			},
			Required: []string{"id"},
		},
	}
	body := []byte(`{"data":{"widget":null}}`)
	failures := gqlassert.AssertResponse(body, op)
	for _, f := range failures {
		if f.Severity == gqlassert.Critical {
			t.Errorf("unexpected critical failure for null selection: %+v", f)
		}
	}
}
