package runplan

import (
	"strings"

	btadapter "github.com/jimbery/bt/internal/adapter"
	gqladapt "github.com/jimbery/bt/internal/adapter/graphql"
	"github.com/jimbery/bt/internal/adapter/openapi"
	"github.com/jimbery/bt/pkg/model"
)

// AdapterForName returns the protocol adapter for the given name (openapi default, graphql).
func AdapterForName(name string) btadapter.Adapter {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "graphql":
		return gqladapt.New()
	default:
		return openapi.New()
	}
}

// AttachResolvedOperations sets Case.ResolvedOperation from the discovered operations list.
func AttachResolvedOperations(cases []model.Case, ops []model.Operation) {
	idx := make(map[string]model.Operation, len(ops))
	for _, op := range ops {
		idx[op.ID] = op
	}
	for i := range cases {
		if op, ok := idx[cases[i].OperationID]; ok {
			opCopy := op
			cases[i].ResolvedOperation = &opCopy
		}
	}
}
