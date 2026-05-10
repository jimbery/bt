package tools

import "github.com/jayimbery/bt/internal/mcp/registry"

// All returns every MCP tool definition for registration and description tests.
func All() []registry.Tool {
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
			Handler:     SuggestStrategyHandler(),
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
