package table_test

import (
	"context"
	"testing"

	"github.com/jayimbery/bt/internal/strategy/table"
	"github.com/jayimbery/bt/pkg/model"
)

type fakeGQLExecutor struct {
	response model.ResponseDetail
	err      error
}

func (f *fakeGQLExecutor) Run(_ context.Context, _ model.CaseInput) (model.ResponseDetail, error) {
	return f.response, f.err
}

func TestTableStrategy_GraphQLCase_RoutedToGQLExecutor(t *testing.T) {
	t.Parallel()
	gqlExec := &fakeGQLExecutor{
		response: model.ResponseDetail{
			StatusCode: 200,
			Body:       []byte(`{"data":{"ping":"pong"}}`),
		},
	}
	restExec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}
	s := table.NewWithOptions(table.Options{GQLExecutor: gqlExec})
	cases := []model.Case{
		{
			ID: "gql-ping",
			Input: model.CaseInput{
				Method:   "POST",
				Path:     "/graphql",
				GQLQuery: `{ ping }`,
			},
			Expected: &model.CaseExpectation{StatusCode: 200},
		},
	}
	results, err := s.Execute(context.Background(), cases, restExec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected GraphQL case to pass, failures: %v", results[0].Failures)
	}
}

func TestTableStrategy_GraphQLCase_NoGQLExecutor_FailsWithClearMessage(t *testing.T) {
	t.Parallel()
	s := table.NewWithOptions(table.Options{GQLExecutor: nil})
	restExec := &fakeExecutor{response: model.ResponseDetail{StatusCode: 200}}
	cases := []model.Case{
		{
			ID: "gql-ping",
			Input: model.CaseInput{
				Method:   "POST",
				Path:     "/graphql",
				GQLQuery: `{ ping }`,
			},
		},
	}
	results, err := s.Execute(context.Background(), cases, restExec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results[0].Passed {
		t.Error("expected GraphQL case to fail when no GQL executor is configured")
	}
	if len(results[0].Failures) == 0 {
		t.Error("expected at least one failure explaining the missing executor")
	}
	msg := results[0].Failures[0].Message
	if msg == "" {
		t.Error("failure message must be non-empty")
	}
}

func TestTableStrategy_RESTCase_NotRoutedToGQLExecutor(t *testing.T) {
	t.Parallel()
	gqlExec := &fakeGQLExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}
	restExec := &fakeExecutor{response: model.ResponseDetail{StatusCode: 200}}
	s := table.NewWithOptions(table.Options{GQLExecutor: gqlExec})
	cases := []model.Case{
		{
			ID:    "rest-get",
			Input: model.CaseInput{Method: "GET", Path: "/orders"},
			Expected: &model.CaseExpectation{StatusCode: 200},
		},
	}
	results, err := s.Execute(context.Background(), cases, restExec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].Passed {
		t.Errorf("expected REST case to pass, failures: %v", results[0].Failures)
	}
}
