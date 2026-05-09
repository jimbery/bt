package strategy

import (
	"context"

	"github.com/jayimbery/bt/pkg/model"
)

// Kind identifies which testing strategy to use.
type Kind string

const (
	KindTable    Kind = "table"
	KindProperty Kind = "property"
	KindFuzz     Kind = "fuzz"
	KindContract Kind = "contract"
	KindGraph    Kind = "graph"
)

// Spec carries the configuration for a single strategy run.
type Spec struct {
	Kind       Kind
	Operations []string
	Invariants []model.Invariant
	Config     map[string]any // each strategy casts this to its own typed config internally
}

// Strategy is the contract every testing strategy must implement.
// Plan must not make network calls.
// Execute must not know about reporting or artifact writing.
type Strategy interface {
	Name() Kind
	Plan(ctx context.Context, spec Spec, ops []model.Operation) ([]model.Case, error)
	Execute(ctx context.Context, cases []model.Case, exec Executor) ([]model.Result, error)
}

// Executor is the minimal interface the engine exposes to strategies.
type Executor interface {
	Run(ctx context.Context, input model.CaseInput) (model.ResponseDetail, error)
}
