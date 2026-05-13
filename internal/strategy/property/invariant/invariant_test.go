package invariant_test

import (
	"strconv"
	"testing"

	"github.com/jayimbery/bt/internal/strategy/property/invariant"
	"github.com/jayimbery/bt/pkg/model"
)

func TestNo5xx(t *testing.T) {
	t.Parallel()
	res := model.Result{Response: model.ResponseDetail{StatusCode: 500}}
	fs := invariant.No5xx(res)
	if len(fs) != 1 || fs[0].Invariant != model.InvariantNo5xx {
		t.Fatalf("unexpected: %v", fs)
	}
	if fs[0].Expected != "< 500" || fs[0].Actual != strconv.Itoa(500) {
		t.Fatalf("expected/actual: %#v", fs[0])
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	if _, ok := invariant.Lookup(model.InvariantNo5xx); !ok {
		t.Fatal("missing no_5xx")
	}
	if _, ok := invariant.Lookup(model.InvariantResponseMatchesSchema); !ok {
		t.Fatal("missing response_matches_schema")
	}
	if _, ok := invariant.Lookup(model.InvariantNoGQLErrors); !ok {
		t.Fatal("missing no_gql_errors")
	}
}
