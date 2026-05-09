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
	if !replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{StatusCode: 500}) {
		t.Fatal("expected mismatch on status")
	}
	if replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{StatusCode: 201}) {
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
	}) {
		t.Fatal("expected header mismatch when missing")
	}
	if replay.FailureStillPresentAfterReplay(f, model.ResponseDetail{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"X-Request-Id": "want"},
	}) {
		t.Fatal("expected no failure when header matches")
	}
}
