package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jayimbery/bt/internal/mcp/registry"
)

const descSuggestStrategy = `bt_suggest_strategy takes operation summaries from bt_discover_operations and returns deterministic strategy recommendations (table, property, fuzz, contract) with rationale for each operation.`

// SuggestStrategyHandler implements the bt_suggest_strategy MCP tool.
func SuggestStrategyHandler() registry.HandlerFunc {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		_ = ctx
		var in struct {
			Operations []struct {
				ID      string `json:"id"`
				Method  string `json:"method"`
				HasBody *bool  `json:"has_body"`
			} `json:"operations"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("decode input: %w", err)
		}
		if in.Operations == nil {
			return nil, fmt.Errorf("operations is required")
		}
		recs := make([]map[string]any, 0, len(in.Operations))
		for _, op := range in.Operations {
			m := strings.ToUpper(strings.TrimSpace(op.Method))
			hasBody := op.HasBody != nil && *op.HasBody
			strategies := suggestForOperation(m, hasBody)
			recs = append(recs, map[string]any{
				"operation_id": op.ID,
				"strategies":   strategies,
			})
		}
		out, err := json.Marshal(map[string]any{"recommendations": recs})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

func suggestForOperation(method string, hasBody bool) []map[string]any {
	contract := map[string]any{"strategy": "contract", "rationale": "Contract checks can validate declared responses for any HTTP operation.", "priority": "optional"}

	add := func(out *[]map[string]any, strategy, rationale, priority string) {
		*out = append(*out, map[string]any{"strategy": strategy, "rationale": rationale, "priority": priority})
	}

	var out []map[string]any
	switch method {
	case "GET", "HEAD", "OPTIONS":
		add(&out, "table", "Table tests exercise explicit examples for read-only endpoints.", "recommended")
		add(&out, "property", "Property-based checks are useful but secondary for simple GETs without bodies.", "optional")
		add(&out, "fuzz", "Fuzzing can still probe edge cases on read-only routes.", "optional")
	case "POST", "PUT":
		if hasBody {
			add(&out, "table", "Table tests cover explicit request/response pairs.", "recommended")
			add(&out, "property", "Stateful writes benefit strongly from property-based testing.", "recommended")
			add(&out, "fuzz", "Fuzzing finds parser and validation bugs on endpoints with bodies.", "recommended")
		} else {
			add(&out, "table", "Table tests exercise explicit examples for endpoints without bodies.", "recommended")
			add(&out, "property", "Property-based checks are optional when no request body is present.", "optional")
			add(&out, "fuzz", "Fuzzing can still probe edge cases.", "optional")
		}
	case "PATCH":
		add(&out, "table", "Table tests cover explicit request/response pairs.", "recommended")
		add(&out, "property", "Partial updates are still worth property checks but are often narrower.", "optional")
		add(&out, "fuzz", "Fuzzing can probe PATCH-specific edge cases.", "optional")
	case "DELETE":
		add(&out, "table", "Table tests can assert deletion outcomes explicitly.", "recommended")
		add(&out, "property", "Property testing is usually not the primary fit for DELETE-only flows.", "not_applicable")
		add(&out, "fuzz", "Fuzzing can still exercise safety guards on delete routes.", "optional")
	default:
		add(&out, "table", "Table tests provide baseline coverage for uncommon methods.", "recommended")
		add(&out, "property", "Property testing may apply depending on semantics.", "optional")
		add(&out, "fuzz", "Fuzzing may still find robustness issues.", "optional")
	}
	out = append(out, contract)
	return out
}
