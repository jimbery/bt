package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	graphqladapt "github.com/jimbery/bt/internal/adapter/graphql"
	"github.com/jimbery/bt/internal/adapter/openapi"
	"github.com/jimbery/bt/internal/ai"
	"github.com/jimbery/bt/internal/ai/parse"
	"github.com/jimbery/bt/internal/ai/prompt"
	"github.com/jimbery/bt/internal/mcp/registry"
	"github.com/jimbery/bt/pkg/model"
)

const descSuggestInvariants = `bt_suggest_invariants Given an operation ID from bt_discover_operations, suggest invariant candidates to add to your bt config. Returns structured suggestions with rationale and confidence for human review. Always includes no_5xx and response_matches_schema. Use bt_discover_operations first to get valid operation IDs.`

// DefaultInvariantStubJSON is used when the model returns unparseable output.
const DefaultInvariantStubJSON = `[
	{"name":"no_5xx","rationale":"Any HTTP 5xx response from this operation indicates a server-side bug.","confidence":"high","invariant_type":"no_5xx"},
	{"name":"response_matches_schema","rationale":"The success response body must conform to the declared OpenAPI schema.","confidence":"high","invariant_type":"response_matches_schema"},
	{"name":"idempotency_key_prevents_duplicates","rationale":"POST operations should respect idempotency keys to prevent duplicate resource creation.","confidence":"medium","invariant_type":"idempotency"}
]`

// SuggestInvariantsHandler implements bt_suggest_invariants. Pass nil provider to use the built-in stub response.
func SuggestInvariantsHandler(p ai.Provider) registry.HandlerFunc {
	if p == nil {
		p = ai.NewStubProvider(DefaultInvariantStubJSON)
	}
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			SchemaPath  string `json:"schema_path"`
			OperationID string `json:"operation_id"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("decode input: %w", err)
		}
		if strings.TrimSpace(in.SchemaPath) == "" {
			return nil, fmt.Errorf("schema_path is required")
		}
		if strings.TrimSpace(in.OperationID) == "" {
			return nil, fmt.Errorf("operation_id is required")
		}
		abs, absErr := filepath.Abs(in.SchemaPath)
		if absErr != nil {
			abs = in.SchemaPath
		}

		ops, discErr := discoverOperationsForSuggest(ctx, abs)
		if discErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": discErr.Error(),
				"code":  "SCHEMA_PARSE_ERROR",
			})
			return out, nil
		}
		var op *model.Operation
		for i := range ops {
			if ops[i].ID == in.OperationID {
				op = &ops[i]
				break
			}
		}
		if op == nil {
			out, _ := json.Marshal(map[string]any{
				"error":        "operation not found",
				"code":         "OPERATION_NOT_FOUND",
				"operation_id": in.OperationID,
			})
			return out, nil
		}

		compReq := prompt.InvariantSuggestionsPrompt(*op)
		compResp, compErr := p.Complete(ctx, compReq)
		label := ai.ProviderLabel(p)
		if compErr != nil {
			log.Printf("bt ai: suggest_invariants provider error: %v", compErr)
			fb, _ := parse.ParseInvariantSuggestions(DefaultInvariantStubJSON)
			b, err := json.Marshal(map[string]any{
				"operation_id": in.OperationID,
				"provider":     label,
				"suggestions":  mergeGraphQLInvariantPrefix(op, fb),
			})
			return json.RawMessage(b), err
		}
		suggestions, perr := parse.ParseInvariantSuggestions(compResp.Text)
		if perr != nil || len(suggestions) == 0 {
			log.Printf("bt ai: suggest_invariants parse failed: %v", perr)
			fb, _ := parse.ParseInvariantSuggestions(DefaultInvariantStubJSON)
			b, err := json.Marshal(map[string]any{
				"operation_id": in.OperationID,
				"provider":     label,
				"suggestions":  mergeGraphQLInvariantPrefix(op, fb),
			})
			return json.RawMessage(b), err
		}
		suggestions = mergeGraphQLInvariantPrefix(op, suggestions)
		b, err := json.Marshal(map[string]any{
			"operation_id": in.OperationID,
			"provider":     label,
			"suggestions":  suggestions,
		})
		return json.RawMessage(b), err
	}
}

func discoverOperationsForSuggest(ctx context.Context, schemaPath string) ([]model.Operation, error) {
	p := strings.ToLower(schemaPath)
	if strings.HasSuffix(p, ".graphql") || strings.HasSuffix(p, ".gql") {
		return graphqladapt.New().Discover(ctx, model.Target{SchemaPath: schemaPath})
	}
	return openapi.New().Discover(ctx, model.Target{SchemaPath: schemaPath})
}

func mergeGraphQLInvariantPrefix(op *model.Operation, base []parse.InvariantSuggestion) []parse.InvariantSuggestion {
	if op == nil || (op.GQLKind != model.GQLQuery && op.GQLKind != model.GQLMutation) {
		return base
	}
	prefix := []parse.InvariantSuggestion{
		{
			Name:          model.InvariantNoGQLErrors,
			Rationale:     "Fails if 'errors' is present and non-empty. Severity is configurable: Critical (default) or Warning.",
			Confidence:    "high",
			InvariantType: "no_gql_errors",
		},
		{
			Name:          model.InvariantResponseMatchesSchema,
			Rationale:     "Validates data.* fields against the SDL-derived selection schema for this operation.",
			Confidence:    "high",
			InvariantType: "response_matches_schema",
		},
	}
	seen := make(map[string]struct{}, len(prefix))
	for _, s := range prefix {
		seen[s.Name] = struct{}{}
	}
	var tail []parse.InvariantSuggestion
	for _, s := range base {
		if _, dup := seen[s.Name]; dup {
			continue
		}
		tail = append(tail, s)
	}
	return append(prefix, tail...)
}
