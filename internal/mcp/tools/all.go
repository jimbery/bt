package tools

import (
	"github.com/jimbery/bt/internal/ai"
	"github.com/jimbery/bt/internal/mcp/registry"
)

// All returns every MCP tool definition with no AI provider (rule-based suggest_strategy).
func All() []registry.Tool {
	return AllWithProvider(nil)
}

// AllWithProvider returns MCP tools, wiring the AI provider into suggest tools when non-nil.
func AllWithProvider(p ai.Provider) []registry.Tool {
	return []registry.Tool{
		{
			Name:        "bt_discover_operations",
			Description: descDiscoverOperations,
			InputSchema: inputDiscoverOperations,
			Handler:     DiscoverOperationsHandler(),
		},
		{
			Name:        "bt_suggest_strategy",
			Description: descSuggestStrategy,
			InputSchema: inputSuggestStrategy,
			Handler:     SuggestStrategyHandler(p),
		},
		{
			Name:        "bt_suggest_invariants",
			Description: descSuggestInvariants,
			InputSchema: inputSuggestInvariants,
			Handler:     SuggestInvariantsHandler(p),
		},
		{
			Name:        "bt_validate",
			Description: descValidate,
			InputSchema: inputValidate,
			Handler:     ValidateHandler(),
		},
		{
			Name:        "bt_scaffold_config",
			Description: descScaffoldConfig,
			InputSchema: inputScaffoldConfig,
			Handler:     ScaffoldConfigHandler(),
		},
		{
			Name:        "bt_run",
			Description: descRun,
			InputSchema: inputRun,
			Handler:     RunHandler(),
		},
		{
			Name:        "bt_explain_failure",
			Description: descExplainFailure,
			InputSchema: inputExplainFailure,
			Handler:     ExplainFailureHandler(),
		},
	}
}
