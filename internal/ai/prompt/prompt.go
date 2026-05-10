package prompt

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/jayimbery/bt/internal/ai"
	"github.com/jayimbery/bt/pkg/model"
)

const maxInvariantUserPrompt = 4000
const maxStrategyUserPrompt = 12000

// InvariantSuggestionsPrompt builds a completion request for bt_suggest_invariants.
func InvariantSuggestionsPrompt(op model.Operation) ai.CompletionRequest {
	sys := strings.Join([]string{
		"You are a backend testing assistant for the bt tool.",
		"Respond with only valid JSON: a single JSON array. No preamble, no markdown fences, no backticks.",
		"Each element must be an object with: name (string), rationale (string, at least 20 characters),",
		"confidence (one of: high, medium, low), invariant_type (one of: no_5xx, response_matches_schema, idempotency, custom),",
		"and optionally config (object) for custom invariants.",
		"Always include no_5xx and response_matches_schema for every operation.",
		"For POST operations with a request body, also suggest idempotency where appropriate.",
		"Suggest at least one custom invariant when the schema implies domain rules (e.g. numeric amount fields matching request/response).",
	}, " ")
	user := buildInvariantUserPrompt(op)
	return ai.CompletionRequest{
		SystemPrompt: sys,
		UserPrompt:   user,
		MaxTokens:    2048,
	}
}

func buildInvariantUserPrompt(op model.Operation) string {
	b, err := json.Marshal(op)
	if err != nil {
		return fmt.Sprintf(`{"id":%q,"method":%q,"path":%q}`, op.ID, op.Method, op.Path)
	}
	if len(b) > maxInvariantUserPrompt {
		b, _ = json.Marshal(slimOperationForPrompt(op))
	}
	return string(b)
}

func slimOperationForPrompt(op model.Operation) map[string]any {
	m := map[string]any{
		"id":     op.ID,
		"method": op.Method,
		"path":   op.Path,
	}
	if len(op.Tags) > 0 {
		m["tags"] = op.Tags
	}
	if len(op.Parameters) > 0 {
		const capParams = 24
		pl := make([]map[string]any, 0, min(capParams, len(op.Parameters)))
		for i, p := range op.Parameters {
			if i >= capParams {
				break
			}
			entry := map[string]any{"name": p.Name, "in": p.In, "required": p.Required}
			if p.Schema != nil {
				entry["schema"] = slimSchemaRef(p.Schema, 0, 3)
			}
			pl = append(pl, entry)
		}
		m["parameters"] = pl
	}
	if op.RequestBody != nil {
		m["request_body"] = slimSchemaRef(op.RequestBody, 0, 4)
	}
	if len(op.Responses) > 0 {
		const capResp = 12
		rl := make([]map[string]any, 0, min(capResp, len(op.Responses)))
		for i, r := range op.Responses {
			if i >= capResp {
				break
			}
			item := map[string]any{"status_code": r.StatusCode}
			if r.Schema != nil {
				item["schema"] = slimSchemaRef(r.Schema, 0, 4)
			}
			rl = append(rl, item)
		}
		m["responses"] = rl
	}
	m["truncated"] = true
	return m
}

func slimSchemaRef(s *model.SchemaRef, depth, maxProps int) map[string]any {
	if s == nil {
		return nil
	}
	if depth > 6 {
		return map[string]any{"type": s.Type}
	}
	out := map[string]any{}
	if s.Type != "" {
		out["type"] = s.Type
	}
	if s.Format != "" {
		out["format"] = s.Format
	}
	if s.Nullable {
		out["nullable"] = true
	}
	if len(s.Enum) > 0 {
		out["enum"] = s.Enum
	}
	if s.Minimum != nil {
		out["minimum"] = *s.Minimum
	}
	if s.Maximum != nil {
		out["maximum"] = *s.Maximum
	}
	if len(s.Required) > 0 {
		out["required"] = s.Required
	}
	if len(s.Properties) > 0 {
		keys := make([]string, 0, len(s.Properties))
		for k := range s.Properties {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		pm := make(map[string]any)
		limit := min(maxProps, len(keys))
		for i := 0; i < limit; i++ {
			k := keys[i]
			pm[k] = slimSchemaRef(s.Properties[k], depth+1, maxProps)
		}
		if len(keys) > limit {
			out["properties_truncated"] = len(keys) - limit
		}
		out["properties"] = pm
	}
	if s.Items != nil {
		out["items"] = slimSchemaRef(s.Items, depth+1, maxProps)
	}
	return out
}

// StrategyRecommendationPrompt builds a completion request for AI-backed bt_suggest_strategy.
func StrategyRecommendationPrompt(ops []model.Operation) ai.CompletionRequest {
	sys := strings.Join([]string{
		"You are a backend testing assistant for the bt tool.",
		"Respond with only valid JSON: a single JSON array. No preamble, no markdown fences, no backticks.",
		"Each element must be an object with operation_id (string) and strategies (array).",
		"Each strategy object must have strategy (table|property|fuzz|contract), priority (recommended|optional|not_applicable), and rationale (non-empty, specific to this operation's schema).",
		"Reference concrete schema characteristics in rationales (fields, enums, numeric constraints) when visible in the input.",
	}, " ")
	b, err := json.Marshal(ops)
	if err != nil {
		b = []byte("[]")
	}
	user := string(b)
	if len(user) > maxStrategyUserPrompt {
		user = user[:maxStrategyUserPrompt]
	}
	return ai.CompletionRequest{
		SystemPrompt: sys,
		UserPrompt:   user,
		MaxTokens:    2048,
	}
}
