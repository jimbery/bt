package runner

import (
	"context"

	gqlrunner "github.com/jayimbery/bt/internal/runner/graphql"
	"github.com/jayimbery/bt/pkg/model"
)

// GQLRESTExecutor routes GraphQL case inputs to gql and everything else to rest.
type GQLRESTExecutor struct {
	rest *Runner
	gql  *gqlrunner.Runner
}

// NewGQLRESTExecutor returns an executor that dispatches by input.IsGraphQL().
func NewGQLRESTExecutor(rest *Runner, gql *gqlrunner.Runner) *GQLRESTExecutor {
	return &GQLRESTExecutor{rest: rest, gql: gql}
}

// Run implements strategy.Executor (same method shape).
func (e *GQLRESTExecutor) Run(ctx context.Context, input model.CaseInput) (model.ResponseDetail, error) {
	if input.IsGraphQL() {
		return e.gql.Run(ctx, input)
	}
	return e.rest.Run(ctx, input)
}
