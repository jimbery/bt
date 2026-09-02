package adapter

import (
	"context"

	"github.com/jimbery/bt/pkg/model"
)

// Adapter is the contract all protocol adapters must implement.
// Adapters own discovery and normalisation only — execution policy
// and test logic live in the engine and strategies.
type Adapter interface {
	// Name returns a short identifier for this adapter, e.g. "openapi".
	Name() string

	// Discover parses the schema referenced by target and returns a
	// normalised slice of Operations. It must not make test requests.
	Discover(ctx context.Context, target model.Target) ([]model.Operation, error)

	// Validate checks that the target schema is parseable and the
	// base URL is reachable. It must not run any test cases.
	Validate(ctx context.Context, target model.Target) error
}
