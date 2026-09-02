package classify_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/jimbery/bt/internal/strategy/fuzz/classify"
	"github.com/jimbery/bt/pkg/model"
)

func opWithStatuses(codes ...int) model.Operation {
	var rs []model.ResponseSpec
	for _, c := range codes {
		rs = append(rs, model.ResponseSpec{
			StatusCode: c,
			Schema:     &model.SchemaRef{Type: "object"},
		})
	}
	return model.Operation{Responses: rs}
}

func resp(code int) *http.Response {
	return &http.Response{StatusCode: code}
}

func TestClassify_NilResponse_IsCrash(t *testing.T) {
	got := classify.Classify(nil, nil, errors.New("connection refused"), opWithStatuses(200))
	if got != classify.ClassificationCrash {
		t.Errorf("expected crash for nil response, got %q", got)
	}
}

func TestClassify_NetworkError_IsCrash(t *testing.T) {
	got := classify.Classify(nil, nil, errors.New("EOF"), opWithStatuses(200))
	if got != classify.ClassificationCrash {
		t.Errorf("expected crash for network error, got %q", got)
	}
}

func TestClassify_500WithStackTrace_IsCrash(t *testing.T) {
	body := []byte("goroutine 1 [running]:\nruntime error: index out of range")
	got := classify.Classify(resp(500), body, nil, opWithStatuses(200, 500))
	if got != classify.ClassificationCrash {
		t.Errorf("expected crash for 500 with stack trace, got %q", got)
	}
}

func TestClassify_TimeoutError_IsTimeout(t *testing.T) {
	got := classify.Classify(nil, nil, classify.ErrTimeout, opWithStatuses(200))
	if got != classify.ClassificationTimeout {
		t.Errorf("expected timeout, got %q", got)
	}
}

func TestClassify_BodyContainsGoroutine_IsValidationLeak(t *testing.T) {
	body := []byte(`{"error":"goroutine 5 [running]: main.handler"}`)
	got := classify.Classify(resp(500), body, nil, opWithStatuses(200, 500))
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak for goroutine in body, got %q", got)
	}
}

func TestClassify_BodyContainsFilePath_IsValidationLeak(t *testing.T) {
	body := []byte(`{"error":"open /var/app/config.yaml: no such file"}`)
	got := classify.Classify(resp(500), body, nil, opWithStatuses(200, 500))
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak for file path in body, got %q", got)
	}
}

func TestClassify_BodyContainsSQLError_IsValidationLeak(t *testing.T) {
	body := []byte(`{"error":"pq: duplicate key value violates unique constraint"}`)
	got := classify.Classify(resp(400), body, nil, opWithStatuses(200, 400))
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak for SQL error in body, got %q", got)
	}
}

func TestClassify_BodyContainsWindowsPath_IsValidationLeak(t *testing.T) {
	body := []byte(`{"error":"open C:\\Users\\app\\config: access denied"}`)
	got := classify.Classify(resp(500), body, nil, opWithStatuses(200, 500))
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak for Windows path in body, got %q", got)
	}
}

func TestClassify_ResponseViolatesSchema_IsSchemaBreak(t *testing.T) {
	op := model.Operation{
		Responses: []model.ResponseSpec{{
			StatusCode: 200,
			Schema: &model.SchemaRef{
				Type:     "object",
				Required: []string{"id", "status"},
				Properties: map[string]*model.SchemaRef{
					"id":     {Type: "string"},
					"status": {Type: "string"},
				},
			},
		}},
	}
	body := []byte(`{"id":"ord-001"}`)
	got := classify.Classify(resp(200), body, nil, op)
	if got != classify.ClassificationSchemaBreak {
		t.Errorf("expected schema_break for missing required field, got %q", got)
	}
}

func TestClassify_ResponseMatchesSchema_IsNotSchemaBreak(t *testing.T) {
	op := model.Operation{
		Responses: []model.ResponseSpec{{
			StatusCode: 200,
			Schema: &model.SchemaRef{
				Type:     "object",
				Required: []string{"id"},
				Properties: map[string]*model.SchemaRef{
					"id": {Type: "string"},
				},
			},
		}},
	}
	body := []byte(`{"id":"ord-001"}`)
	got := classify.Classify(resp(200), body, nil, op)
	if got != classify.ClassificationPass {
		t.Errorf("expected pass for valid body, got %q", got)
	}
}

func TestClassify_UndeclaredStatusCode_IsUnexpectedStatus(t *testing.T) {
	got := classify.Classify(resp(422), []byte(`{"error":"unprocessable"}`), nil, opWithStatuses(200, 400))
	if got != classify.ClassificationUnexpectedStatus {
		t.Errorf("expected unexpected_status for 422, got %q", got)
	}
}

func TestClassify_GraphQLUndeclared404_IsPass(t *testing.T) {
	op := model.Operation{
		GQLDocument: `query { ping }`,
		Responses:   []model.ResponseSpec{{StatusCode: 200, Schema: &model.SchemaRef{Type: "object"}}},
	}
	got := classify.Classify(resp(404), []byte(`not found`), nil, op)
	if got != classify.ClassificationPass {
		t.Errorf("expected pass for graphql op with undeclared 404, got %q", got)
	}
}

func TestClassify_DeclaredStatusCode_IsNotUnexpectedStatus(t *testing.T) {
	got := classify.Classify(resp(400), []byte(`{"error":"bad request","code":"BAD"}`), nil, opWithStatuses(200, 400))
	if got == classify.ClassificationUnexpectedStatus {
		t.Error("expected not unexpected_status for a declared 400, got unexpected_status")
	}
}

func TestClassify_CleanResponse_IsPass(t *testing.T) {
	op := model.Operation{
		Responses: []model.ResponseSpec{{
			StatusCode: 200,
			Schema: &model.SchemaRef{
				Type:     "object",
				Required: []string{"id"},
				Properties: map[string]*model.SchemaRef{
					"id": {Type: "string"},
				},
			},
		}},
	}
	body := []byte(`{"id":"ord-001"}`)
	got := classify.Classify(resp(200), body, nil, op)
	if got != classify.ClassificationPass {
		t.Errorf("expected pass for clean response, got %q", got)
	}
}

func TestClassify_CrashTakesPriorityOverValidationLeak(t *testing.T) {
	body := []byte("goroutine 1 [running]:")
	got := classify.Classify(nil, body, errors.New("EOF"), opWithStatuses(200))
	if got != classify.ClassificationCrash {
		t.Errorf("expected crash to take priority over validation_leak, got %q", got)
	}
}

func TestClassify_ValidationLeakTakesPriorityOverSchemaBreak(t *testing.T) {
	op := model.Operation{
		Responses: []model.ResponseSpec{{
			StatusCode: 500,
			Schema: &model.SchemaRef{
				Type:     "object",
				Required: []string{"id"},
				Properties: map[string]*model.SchemaRef{
					"id": {Type: "string"},
				},
			},
		}},
	}
	body := []byte(`{"error":"goroutine 1 [running]"}`)
	got := classify.Classify(resp(500), body, nil, op)
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak to take priority over schema_break, got %q", got)
	}
}

func TestClassify_NilBody_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Classify panicked on nil body: %v", r)
		}
	}()
	classify.Classify(resp(200), nil, nil, opWithStatuses(200))
}

func TestClassify_EmptyBody_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Classify panicked on empty body: %v", r)
		}
	}()
	classify.Classify(resp(200), []byte{}, nil, opWithStatuses(200))
}
