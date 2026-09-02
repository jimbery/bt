package gqlcase_test

import (
	"testing"

	"github.com/jimbery/bt/internal/gqlcase"
	"github.com/jimbery/bt/pkg/model"
)

func TestMinimalInput_GraphQLOp(t *testing.T) {
	t.Parallel()
	op := model.Operation{
		ID:          "me",
		Method:      "POST",
		Path:        "/graphql",
		GQLKind:     model.GQLQuery,
		GQLDocument: "query { me { id } }",
		Responses:   []model.ResponseSpec{{StatusCode: 200}},
	}
	in := gqlcase.MinimalInput(op)
	if !in.IsGraphQL() {
		t.Fatal("expected GraphQL case input")
	}
	if in.GQLQuery != op.GQLDocument {
		t.Errorf("GQLQuery: got %q", in.GQLQuery)
	}
}
