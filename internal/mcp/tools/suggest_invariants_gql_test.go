package tools_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/jayimbery/bt/internal/ai"
	"github.com/jayimbery/bt/internal/mcp/tools"
	"github.com/jayimbery/bt/internal/testutil"
	"github.com/jayimbery/bt/pkg/model"
)

func TestSuggestInvariants_GraphQLQuery_IncludesGQLInvariants(t *testing.T) {
	root := testutil.RepoRoot(t)
	schema := filepath.Join(root, "examples/graphql-api/schema.graphql")
	h := tools.SuggestInvariantsHandler(ai.NewStubProvider(`[{"name":"no_5xx","rationale":"x","confidence":"high","invariant_type":"no_5xx"}]`))
	raw, err := h(context.Background(), mustJSON(t, map[string]string{
		"schema_path":  schema,
		"operation_id": "orders",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var out struct {
		Suggestions []struct {
			Name string `json:"name"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	names := suggestionNames(out.Suggestions)
	if !containsStr(names, model.InvariantNoGQLErrors) {
		t.Errorf("expected no_gql_errors in suggestions, got %v", names)
	}
	if !containsStr(names, model.InvariantResponseMatchesSchema) {
		t.Errorf("expected response_matches_schema in suggestions, got %v", names)
	}
}

func TestSuggestInvariants_GraphQL_GQLSuggestionsAppearFirst(t *testing.T) {
	root := testutil.RepoRoot(t)
	schema := filepath.Join(root, "examples/graphql-api/schema.graphql")
	h := tools.SuggestInvariantsHandler(ai.NewStubProvider(`[{"name":"no_5xx","rationale":"x","confidence":"high","invariant_type":"no_5xx"}]`))
	raw, err := h(context.Background(), mustJSON(t, map[string]string{
		"schema_path":  schema,
		"operation_id": "createOrder",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var out struct {
		Suggestions []struct {
			Name string `json:"name"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.Suggestions) < 2 {
		t.Fatalf("expected at least 2 suggestions, got %d", len(out.Suggestions))
	}
	first := out.Suggestions[0].Name
	second := out.Suggestions[1].Name
	if first != model.InvariantNoGQLErrors && first != model.InvariantResponseMatchesSchema {
		t.Errorf("expected first suggestion to be GQL-specific, got %q", first)
	}
	if second != model.InvariantNoGQLErrors && second != model.InvariantResponseMatchesSchema {
		t.Errorf("expected second suggestion to be GQL-specific, got %q", second)
	}
	if first == second {
		t.Errorf("first two suggestions should differ")
	}
}

func TestSuggestInvariants_OpenAPI_DoesNotIncludeGQLInvariants(t *testing.T) {
	root := testutil.RepoRoot(t)
	schema := filepath.Join(root, "examples/orders-api/spec/openapi.yaml")
	h := tools.SuggestInvariantsHandler(ai.NewStubProvider(tools.DefaultInvariantStubJSON))
	raw, err := h(context.Background(), mustJSON(t, map[string]string{
		"schema_path":  schema,
		"operation_id": "GetHealth",
	}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	var out struct {
		Suggestions []struct {
			Name string `json:"name"`
		} `json:"suggestions"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	names := suggestionNames(out.Suggestions)
	if containsStr(names, model.InvariantNoGQLErrors) {
		t.Errorf("REST operation should not get no_gql_errors, got %v", names)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func suggestionNames(s []struct {
	Name string `json:"name"`
}) []string {
	out := make([]string, len(s))
	for i := range s {
		out[i] = s[i].Name
	}
	return out
}

func containsStr(slice []string, want string) bool {
	for _, x := range slice {
		if x == want {
			return true
		}
	}
	return false
}
