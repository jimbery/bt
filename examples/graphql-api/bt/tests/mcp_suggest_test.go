//go:build integration

package tests_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/ai"
	"github.com/jimbery/bt/internal/mcp/tools"
	"github.com/jimbery/bt/internal/testutil"
	"github.com/jimbery/bt/pkg/model"
)

func TestMCPSuggestInvariants_GraphQLQuery_ReturnsGQLSuggestionsFirst(t *testing.T) {
	root := testutil.RepoRoot(t)
	schema := filepath.Join(root, "examples/graphql-api/schema.graphql")
	h := tools.SuggestInvariantsHandler(ai.NewStubProvider(`[{"name":"no_5xx","rationale":"x","confidence":"high","invariant_type":"no_5xx"}]`))
	raw, err := h(context.Background(), testutil.MustJSON(t, map[string]string{
		"schema_path":  schema,
		"operation_id": "order",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var out struct {
		Suggestions []map[string]any `json:"suggestions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !hasSuggestionNamedMap(out.Suggestions, model.InvariantNoGQLErrors) {
		t.Errorf("expected no_gql_errors in suggestions; got: %v", suggestionNamesMap(out.Suggestions))
	}
	if !hasSuggestionNamedMap(out.Suggestions, model.InvariantResponseMatchesSchema) {
		t.Errorf("expected response_matches_schema in suggestions; got: %v", suggestionNamesMap(out.Suggestions))
	}

	gqlNames := []string{model.InvariantNoGQLErrors, model.InvariantResponseMatchesSchema}
	firstNonGQL := -1
	firstGQL := -1
	for i, s := range out.Suggestions {
		name, _ := s["name"].(string)
		isGQL := false
		for _, g := range gqlNames {
			if name == g {
				isGQL = true
				break
			}
		}
		if isGQL && firstGQL == -1 {
			firstGQL = i
		}
		if !isGQL && firstNonGQL == -1 {
			firstNonGQL = i
		}
	}
	if firstGQL > firstNonGQL && firstNonGQL != -1 {
		t.Errorf("GQL suggestions (first at index %d) must appear before non-GQL suggestions (first at index %d)",
			firstGQL, firstNonGQL)
	}

	for _, s := range out.Suggestions {
		name, _ := s["name"].(string)
		if name != model.InvariantNoGQLErrors {
			continue
		}
		rat, _ := s["rationale"].(string)
		rat = strings.ToLower(rat)
		if !strings.Contains(rat, "warning") && !strings.Contains(rat, "configurable") {
			t.Errorf("no_gql_errors rationale should mention severity configurability; got: %q", s["rationale"])
		}
		return
	}
	t.Error("no_gql_errors suggestion not found")
}

func TestMCPSuggestInvariants_GraphQLMutation_ReturnsGQLSuggestionsFirst(t *testing.T) {
	root := testutil.RepoRoot(t)
	schema := filepath.Join(root, "examples/graphql-api/schema.graphql")
	h := tools.SuggestInvariantsHandler(ai.NewStubProvider(`[{"name":"no_5xx","rationale":"x","confidence":"high","invariant_type":"no_5xx"}]`))
	raw, err := h(context.Background(), testutil.MustJSON(t, map[string]string{
		"schema_path":  schema,
		"operation_id": "createOrder",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var out struct {
		Suggestions []map[string]any `json:"suggestions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !hasSuggestionNamedMap(out.Suggestions, model.InvariantNoGQLErrors) {
		t.Errorf("expected no_gql_errors for Mutation; got: %v", suggestionNamesMap(out.Suggestions))
	}
	if !hasSuggestionNamedMap(out.Suggestions, model.InvariantResponseMatchesSchema) {
		t.Errorf("expected response_matches_schema for Mutation; got: %v", suggestionNamesMap(out.Suggestions))
	}
}

func TestMCPSuggestInvariants_Subscription_NoPropertyGQLInvariants(t *testing.T) {
	root := testutil.RepoRoot(t)
	schema := filepath.Join(root, "examples/graphql-api/bt/tests/testdata/schema_subscription.graphql")
	h := tools.SuggestInvariantsHandler(ai.NewStubProvider(`[{"name":"no_5xx","rationale":"x","confidence":"high","invariant_type":"no_5xx"}]`))
	raw, err := h(context.Background(), testutil.MustJSON(t, map[string]string{
		"schema_path":  schema,
		"operation_id": "orderUpdated",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var out struct {
		Suggestions []map[string]any `json:"suggestions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if hasSuggestionNamedMap(out.Suggestions, model.InvariantNoGQLErrors) {
		t.Error("no_gql_errors must not be suggested for Subscription operations")
	}
	if hasSuggestionNamedMap(out.Suggestions, model.InvariantResponseMatchesSchema) {
		t.Error("response_matches_schema must not be suggested for Subscription operations")
	}
}

func hasSuggestionNamedMap(suggestions []map[string]any, name string) bool {
	for _, s := range suggestions {
		if n, ok := s["name"].(string); ok && n == name {
			return true
		}
	}
	return false
}

func suggestionNamesMap(suggestions []map[string]any) []string {
	out := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		if n, ok := s["name"].(string); ok {
			out = append(out, n)
		}
	}
	return out
}
