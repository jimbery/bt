package replay_test

import (
	"net/http"
	"testing"

	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/pkg/model"
)

func TestFailureStillPresentAfterReplay_StatusCode(t *testing.T) {
	t.Parallel()
	f := model.Failure{
		Invariant: model.InvariantStatusCode,
		Expected:  201,
	}
	if !replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{StatusCode: 500}, nil) {
		t.Fatal("expected mismatch on status")
	}
	if replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{StatusCode: 201}, nil) {
		t.Fatal("expected no failure when status matches")
	}
}

func TestFailureStillPresentAfterReplay_ResponseHeader(t *testing.T) {
	t.Parallel()
	f := model.Failure{
		Invariant: model.InvariantResponseHeader,
		Message:   `header "X-Request-ID": expected "want", got ""`,
		Expected:  "want",
	}
	if !replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{},
	}, nil) {
		t.Fatal("expected header mismatch when missing")
	}
	if replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"X-Request-Id": "want"},
	}, nil) {
		t.Fatal("expected no failure when header matches")
	}
}

func TestFailureStillPresentAfterReplay_ResponseSchema(t *testing.T) {
	t.Parallel()
	f := model.Failure{
		Invariant: model.InvariantResponseMatchesSchema,
		Message:   "wrong type",
	}
	exp := &model.CaseExpectation{
		Schema: &model.SchemaRef{
			Type: "object",
			Properties: map[string]*model.SchemaRef{
				"id": {Type: "string"},
			},
			Required: []string{"id"},
		},
	}
	if !replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{
		StatusCode: 200,
		Body:       []byte(`{"id":1}`),
	}, exp) {
		t.Fatal("expected schema failure still present")
	}
	if replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{
		StatusCode: 200,
		Body:       []byte(`{"id":"ok"}`),
	}, exp) {
		t.Fatal("expected schema failure cleared")
	}
}
