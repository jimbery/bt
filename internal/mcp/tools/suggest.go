package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/jayimbery/bt/internal/ai"
	"github.com/jayimbery/bt/internal/ai/parse"
	"github.com/jayimbery/bt/internal/ai/prompt"
	"github.com/jayimbery/bt/internal/mcp/registry"
	"github.com/jayimbery/bt/pkg/model"
)

const descSuggestStrategy = `bt_suggest_strategy takes operation summaries from bt_discover_operations and returns strategy recommendations (table, property, fuzz, contract) with rationale for each operation. With an AI API key configured, responses use an AI model; otherwise deterministic rules are used.`

// SuggestStrategyHandler implements the bt_suggest_strategy MCP tool.
// Pass nil provider to use rule-based recommendations only.
func SuggestStrategyHandler(p ai.Provider) registry.HandlerFunc {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
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

		ruleRecs := ruleBasedRecommendations(in.Operations)
		provider := "rules"

		if p != nil {
			mini := make([]model.Operation, 0, len(in.Operations))
			for _, o := range in.Operations {
				m := strings.ToUpper(strings.TrimSpace(o.Method))
				hasBody := o.HasBody != nil && *o.HasBody
				op := model.Operation{
					ID:     o.ID,
					Method: m,
					Path:   "",
				}
				if hasBody {
					op.RequestBody = &model.SchemaRef{Type: "object"}
				}
				mini = append(mini, op)
			}
			req := prompt.StrategyRecommendationPrompt(mini)
			comp, compErr := p.Complete(ctx, req)
			if compErr == nil && strings.TrimSpace(comp.Text) != "" {
				parsed, perr := parse.ParseStrategyRecommendations(comp.Text)
				if perr == nil && strategyRecsCoverInput(parsed, in.Operations) {
					out := make([]map[string]any, 0, len(parsed))
					for _, r := range parsed {
						strats := make([]map[string]any, 0, len(r.Strategies))
						for _, s := range r.Strategies {
							strats = append(strats, map[string]any{
								"strategy":  s.Strategy,
								"priority":  s.Priority,
								"rationale": s.Rationale,
							})
						}
						out = append(out, map[string]any{
							"operation_id": r.OperationID,
							"strategies":   strats,
						})
					}
					b, marshalErr := json.Marshal(map[string]any{
						"recommendations": out,
						"provider":        ai.ProviderLabel(p),
					})
					if marshalErr == nil {
						return json.RawMessage(b), nil
					}
					log.Printf("bt ai: suggest_strategy marshal: %v", marshalErr)
				}
			} else if compErr != nil {
				log.Printf("bt ai: suggest_strategy provider error: %v", compErr)
			} else {
				log.Printf("bt ai: suggest_strategy falling back to rules (empty model text)")
			}
			log.Printf("bt ai: suggest_strategy using rule-based recommendations")
		}

		b, err := json.Marshal(map[string]any{
			"recommendations": ruleRecs,
			"provider":        provider,
		})
		if err != nil {
			return nil, err
		}
		return json.RawMessage(b), nil
	}
}

func strategyRecsCoverInput(parsed []parse.StrategyRec, summaries []struct {
	ID      string `json:"id"`
	Method  string `json:"method"`
	HasBody *bool  `json:"has_body"`
}) bool {
	if len(parsed) != len(summaries) {
		return false
	}
	seen := make(map[string]bool, len(parsed))
	for _, r := range parsed {
		seen[r.OperationID] = true
	}
	for _, o := range summaries {
		if !seen[o.ID] {
			return false
		}
	}
	return true
}

func ruleBasedRecommendations(ops []struct {
	ID      string `json:"id"`
	Method  string `json:"method"`
	HasBody *bool  `json:"has_body"`
}) []map[string]any {
	recs := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		m := strings.ToUpper(strings.TrimSpace(op.Method))
		hasBody := op.HasBody != nil && *op.HasBody
		strategies := suggestForOperation(m, hasBody)
		recs = append(recs, map[string]any{
			"operation_id": op.ID,
			"strategies":   strategies,
		})
	}
	return recs
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
