//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func btBinaryForAIScaffold(t *testing.T) string {
	t.Helper()
	p := filepath.Join(findRepoRoot(t), "bt")
	if _, err := os.Stat(p); err != nil {
		t.Skip("bt binary not found; build with: go build -o bt ./cmd/bt")
	}
	return p
}

func ordersSchemaRelativePath() string {
	return filepath.Join("examples", "orders-api", "spec", "openapi.yaml")
}

func defaultMCPConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(findRepoRoot(t), "examples", "orders-api", "bt", "backendtest.yaml")
}

// envWithoutAnthropicKey returns a copy of env with ANTHROPIC_API_KEY removed so
// bt mcp serve uses stub/rules for suggest tools (CI and local parity).
func envWithoutAnthropicKey(env []string) []string {
	out := make([]string, 0, len(env))
	for _, e := range env {
		if strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			continue
		}
		out = append(out, e)
	}
	return out
}

func callMCPTool(t *testing.T, tool string, input map[string]any, env []string) map[string]any {
	t.Helper()
	inputJSON, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	root := findRepoRoot(t)
	bt := btBinaryForAIScaffold(t)
	cmd := exec.Command(bt, "mcp", "call", tool,
		"--input", string(inputJSON),
		"--output", "json",
		"--config", defaultMCPConfigPath(t),
	)
	cmd.Dir = root
	if env != nil {
		cmd.Env = env
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bt mcp call %s failed: %v\noutput: %s", tool, err, out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("bt mcp call %s returned invalid JSON: %v\nraw: %s", tool, err, out)
	}
	return resp
}

func TestAIScaffold_SuggestInvariants_CreateOrder_ReturnsValidShape(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_invariants", map[string]any{
		"schema_path":  ordersSchemaRelativePath(),
		"operation_id": "CreateOrder",
	}, nil)

	if resp["operation_id"] != "CreateOrder" {
		t.Errorf("expected operation_id=CreateOrder, got %v", resp["operation_id"])
	}
	if resp["provider"] == nil || resp["provider"] == "" {
		t.Error("expected provider field to be set")
	}
	suggestions, ok := resp["suggestions"].([]any)
	if !ok {
		t.Fatal("expected suggestions array")
	}
	if len(suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}
}

func TestAIScaffold_SuggestInvariants_CreateOrder_SuggestionsHaveRequiredFields(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_invariants", map[string]any{
		"schema_path":  ordersSchemaRelativePath(),
		"operation_id": "CreateOrder",
	}, nil)

	suggestions, _ := resp["suggestions"].([]any)
	for i, s := range suggestions {
		sm, ok := s.(map[string]any)
		if !ok {
			t.Errorf("suggestion[%d] is not a map", i)
			continue
		}
		for _, field := range []string{"name", "rationale", "confidence", "invariant_type"} {
			if v, ok := sm[field]; !ok || v == "" {
				t.Errorf("suggestion[%d] missing or empty field %q", i, field)
			}
		}
	}
}

func TestAIScaffold_SuggestInvariants_CreateOrder_ConfidenceIsValidEnum(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_invariants", map[string]any{
		"schema_path":  ordersSchemaRelativePath(),
		"operation_id": "CreateOrder",
	}, nil)

	valid := map[string]bool{"high": true, "medium": true, "low": true}
	suggestions, _ := resp["suggestions"].([]any)
	for i, s := range suggestions {
		sm, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("suggestion[%d] is not a map", i)
		}
		confidence, _ := sm["confidence"].(string)
		if !valid[confidence] {
			t.Errorf("suggestion[%d] has invalid confidence %q", i, confidence)
		}
	}
}

func TestAIScaffold_SuggestInvariants_CreateOrder_InvariantTypeIsValidEnum(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_invariants", map[string]any{
		"schema_path":  ordersSchemaRelativePath(),
		"operation_id": "CreateOrder",
	}, nil)

	valid := map[string]bool{
		"no_5xx":                  true,
		"response_matches_schema": true,
		"idempotency":             true,
		"custom":                  true,
	}
	suggestions, _ := resp["suggestions"].([]any)
	for i, s := range suggestions {
		sm, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("suggestion[%d] is not a map", i)
		}
		invariantType, _ := sm["invariant_type"].(string)
		if !valid[invariantType] {
			t.Errorf("suggestion[%d] has invalid invariant_type %q", i, invariantType)
		}
	}
}

func TestAIScaffold_SuggestInvariants_GetOrderBroken_ReturnsValidShape(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_invariants", map[string]any{
		"schema_path":  ordersSchemaRelativePath(),
		"operation_id": "GetOrderBroken",
	}, nil)

	if resp["operation_id"] != "GetOrderBroken" {
		t.Errorf("expected operation_id=GetOrderBroken, got %v", resp["operation_id"])
	}
	suggestions, ok := resp["suggestions"].([]any)
	if !ok || len(suggestions) == 0 {
		t.Error("expected at least one suggestion for GetOrderBroken")
	}
}

func TestAIScaffold_SuggestInvariants_UnknownOperation_ReturnsStructuredError(t *testing.T) {
	inputJSON, err := json.Marshal(map[string]any{
		"schema_path":  ordersSchemaRelativePath(),
		"operation_id": "OperationThatDoesNotExist",
	})
	if err != nil {
		t.Fatal(err)
	}
	root := findRepoRoot(t)
	bt := btBinaryForAIScaffold(t)
	cmd := exec.Command(bt, "mcp", "call", "bt_suggest_invariants",
		"--input", string(inputJSON),
		"--output", "json",
		"--config", defaultMCPConfigPath(t),
	)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("bt mcp call: %v\n%s", err, out)
	}
	var resp map[string]any
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if resp["code"] != "OPERATION_NOT_FOUND" {
		t.Errorf("expected code OPERATION_NOT_FOUND, got %v", resp["code"])
	}
}

func TestAIScaffold_SuggestInvariants_RationaleIsNonEmpty(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_invariants", map[string]any{
		"schema_path":  ordersSchemaRelativePath(),
		"operation_id": "CreateOrder",
	}, nil)
	suggestions, _ := resp["suggestions"].([]any)
	for i, s := range suggestions {
		sm, ok := s.(map[string]any)
		if !ok {
			t.Fatalf("suggestion[%d] is not a map", i)
		}
		rationale, _ := sm["rationale"].(string)
		if rationale == "" {
			t.Errorf("suggestion[%d] has empty rationale", i)
		}
		if len(rationale) < 20 {
			t.Errorf("suggestion[%d] rationale is suspiciously short (%d chars): %q", i, len(rationale), rationale)
		}
	}
}

func TestAIScaffold_SuggestStrategy_ReturnsProviderField(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_strategy", map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	}, nil)
	if resp["provider"] == nil || resp["provider"] == "" {
		t.Error("expected provider field in bt_suggest_strategy response")
	}
}

func TestAIScaffold_SuggestStrategy_NoAPIKey_UsesRulesOrStub(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_strategy", map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	}, envWithoutAnthropicKey(os.Environ()))
	provider, _ := resp["provider"].(string)
	if provider == "ai" {
		t.Error("expected provider to be 'rules' or 'stub' when ANTHROPIC_API_KEY is unset for this subprocess")
	}
}

func TestAIScaffold_SuggestStrategy_ReturnsRationaleForAllStrategies(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_strategy", map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	}, envWithoutAnthropicKey(os.Environ()))
	recs, _ := resp["recommendations"].([]any)
	for _, rec := range recs {
		rm, ok := rec.(map[string]any)
		if !ok {
			t.Fatal("recommendation is not a map")
		}
		strategies, _ := rm["strategies"].([]any)
		for i, s := range strategies {
			sm, ok := s.(map[string]any)
			if !ok {
				t.Fatalf("strategy[%d] is not a map", i)
			}
			rationale, _ := sm["rationale"].(string)
			if rationale == "" {
				t.Errorf("strategy[%d] has empty rationale in stub/rules path", i)
			}
		}
	}
}

func TestAIScaffold_SuggestThenValidate_SuggestionsAreUsableInConfig(t *testing.T) {
	resp := callMCPTool(t, "bt_suggest_invariants", map[string]any{
		"schema_path":  ordersSchemaRelativePath(),
		"operation_id": "CreateOrder",
	}, nil)
	suggestions, _ := resp["suggestions"].([]any)
	if len(suggestions) == 0 {
		t.Skip("no suggestions returned — cannot test config self-consistency")
	}

	invariantNames := make([]string, 0, len(suggestions))
	for _, s := range suggestions {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := sm["name"].(string); ok && name != "" {
			invariantNames = append(invariantNames, name)
		}
	}
	if len(invariantNames) == 0 {
		t.Fatal("expected at least one suggestion name")
	}

	scaffoldResp := callMCPTool(t, "bt_scaffold_config", map[string]any{
		"schema_path": ordersSchemaRelativePath(),
		"base_url":    "http://localhost:8080",
		"strategies":  []string{"property"},
	}, nil)

	configYAML, _ := scaffoldResp["config_yaml"].(string)
	if configYAML == "" {
		t.Skip("scaffold returned empty config — cannot test self-consistency")
	}

	var rootDoc map[string]any
	if err := yaml.Unmarshal([]byte(configYAML), &rootDoc); err != nil {
		t.Fatalf("parse scaffold yaml: %v", err)
	}
	strats, ok := rootDoc["strategies"].([]any)
	if !ok {
		t.Fatal("expected strategies in scaffold yaml")
	}
	foundProperty := false
	for _, st := range strats {
		sm, ok := st.(map[string]any)
		if !ok {
			continue
		}
		if sm["type"] != "property" {
			continue
		}
		foundProperty = true
		inv := make([]any, len(invariantNames))
		for i, n := range invariantNames {
			inv[i] = n
		}
		sm["invariants"] = inv
	}
	if !foundProperty {
		t.Fatal("expected a property strategy block in scaffold output")
	}

	modified, err := yaml.Marshal(rootDoc)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	tmpPath := filepath.Join(t.TempDir(), "test-config.yaml")
	if err := os.WriteFile(tmpPath, modified, 0o644); err != nil {
		t.Fatalf("cannot write temp config: %v", err)
	}

	validateResp := callMCPTool(t, "bt_validate", map[string]any{
		"config_path": tmpPath,
	}, nil)
	if valid, ok := validateResp["valid"].(bool); !ok || !valid {
		t.Errorf("config with suggested invariant names failed validation: %v", validateResp["errors"])
	}
}
