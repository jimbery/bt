//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/mcp/testclient"
	"github.com/jayimbery/bt/internal/testutil"
)

func newMCPClient(t *testing.T) *testclient.Client {
	t.Helper()
	root := testutil.RepoRoot(t)
	bin := filepath.Join(root, "bt")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("bt binary not present; build with: go build -o bt ./cmd/bt")
	}
	defaultCfg := filepath.Join(root, "examples", "orders-api", "bt", "backendtest.yaml")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	client, err := testclient.Start(ctx, bin, defaultCfg)
	if err != nil {
		t.Fatalf("failed to start MCP server: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func ordersSchemaPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(testutil.RepoRoot(t), "examples", "orders-api", "spec", "openapi.yaml")
}

func ordersConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(testutil.RepoRoot(t), "examples", "orders-api", "bt", "backendtest.yaml")
}

func ordersFailuresConfigPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(testutil.RepoRoot(t), "examples", "orders-api", "bt", "backendtest-failures.yaml")
}

func call(t *testing.T, client *testclient.Client, tool string, input map[string]any) map[string]any {
	t.Helper()
	result, err := client.Call(context.Background(), tool, input)
	if err != nil {
		t.Fatalf("tool %q returned error: %v", tool, err)
	}
	var resp map[string]any
	if err := json.Unmarshal(result, &resp); err != nil {
		t.Fatalf("tool %q returned invalid JSON: %v\nraw: %s", tool, err, result)
	}
	return resp
}

func TestMCPIntegration_DiscoverOperations_ReturnsOrdersAPIOperations(t *testing.T) {
	client := newMCPClient(t)
	resp := call(t, client, "bt_discover_operations", map[string]any{
		"schema_path": ordersSchemaPath(t),
	})

	count, ok := resp["operation_count"].(float64)
	if !ok {
		t.Fatalf("expected operation_count in response, got: %v", resp)
	}
	if int(count) == 0 {
		t.Error("expected at least one operation from orders API schema")
	}

	ops, ok := resp["operations"].([]any)
	if !ok {
		t.Fatal("expected operations array in response")
	}
	if len(ops) != int(count) {
		t.Errorf("operation_count=%d but operations has %d entries", int(count), len(ops))
	}
}

func TestMCPIntegration_DiscoverOperations_OperationsHaveRequiredFields(t *testing.T) {
	client := newMCPClient(t)
	resp := call(t, client, "bt_discover_operations", map[string]any{
		"schema_path": ordersSchemaPath(t),
	})

	ops := resp["operations"].([]any)
	for i, op := range ops {
		opMap, ok := op.(map[string]any)
		if !ok {
			t.Errorf("operation[%d] is not a map", i)
			continue
		}
		for _, field := range []string{"id", "method", "path"} {
			if v, ok := opMap[field]; !ok || v == "" {
				t.Errorf("operation[%d] missing or empty field %q: %v", i, field, opMap)
			}
		}
	}
}

func TestMCPIntegration_DiscoverOperations_FindsKnownEndpoints(t *testing.T) {
	client := newMCPClient(t)
	resp := call(t, client, "bt_discover_operations", map[string]any{
		"schema_path": ordersSchemaPath(t),
	})

	ops := resp["operations"].([]any)
	paths := map[string]bool{}
	for _, op := range ops {
		opMap := op.(map[string]any)
		paths[opMap["path"].(string)] = true
	}

	expected := []string{"/health", "/orders", "/orders/{id}"}
	for _, p := range expected {
		if !paths[p] {
			t.Errorf("expected path %q in discovered operations, found: %v", p, paths)
		}
	}
}

func TestMCPIntegration_SuggestStrategy_ReturnsRecommendationsForAllOperations(t *testing.T) {
	client := newMCPClient(t)

	discoverResp := call(t, client, "bt_discover_operations", map[string]any{
		"schema_path": ordersSchemaPath(t),
	})
	ops := discoverResp["operations"].([]any)

	opInputs := make([]map[string]any, 0, len(ops))
	for _, op := range ops {
		opMap := op.(map[string]any)
		opInputs = append(opInputs, map[string]any{
			"id":       opMap["id"],
			"method":   opMap["method"],
			"has_body": opMap["has_body"],
		})
	}

	resp := call(t, client, "bt_suggest_strategy", map[string]any{
		"operations": opInputs,
	})

	recs, ok := resp["recommendations"].([]any)
	if !ok {
		t.Fatal("expected recommendations array")
	}
	if len(recs) != len(ops) {
		t.Errorf("expected %d recommendations (one per operation), got %d", len(ops), len(recs))
	}
}

func TestMCPIntegration_SuggestStrategy_CreateOrder_RecommendsProperty(t *testing.T) {
	client := newMCPClient(t)
	resp := call(t, client, "bt_suggest_strategy", map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})

	recs := resp["recommendations"].([]any)
	if len(recs) == 0 {
		t.Fatal("expected at least one recommendation")
	}
	rec := recs[0].(map[string]any)
	strategies := rec["strategies"].([]any)

	propertyRecommended := false
	for _, s := range strategies {
		sm := s.(map[string]any)
		if sm["strategy"] == "property" && sm["priority"] == "recommended" {
			propertyRecommended = true
		}
	}
	if !propertyRecommended {
		t.Error("expected property strategy to be recommended for POST CreateOrder")
	}
}

func TestMCPIntegration_SuggestStrategy_Recommendations_HaveRationale(t *testing.T) {
	client := newMCPClient(t)
	resp := call(t, client, "bt_suggest_strategy", map[string]any{
		"operations": []any{
			map[string]any{"id": "GetHealth", "method": "GET", "has_body": false},
		},
	})

	recs := resp["recommendations"].([]any)
	rec := recs[0].(map[string]any)
	strategies := rec["strategies"].([]any)
	for _, s := range strategies {
		sm := s.(map[string]any)
		if sm["rationale"] == "" {
			t.Errorf("strategy %q has empty rationale", sm["strategy"])
		}
	}
}

func TestMCPIntegration_Validate_OrdersAPIConfig_IsValid(t *testing.T) {
	client := newMCPClient(t)
	resp := call(t, client, "bt_validate", map[string]any{
		"config_path": ordersConfigPath(t),
	})

	if valid, ok := resp["valid"].(bool); !ok || !valid {
		t.Errorf("expected valid=true for orders API config, got: %v", resp)
	}
	if errors, ok := resp["errors"].([]any); ok && len(errors) != 0 {
		t.Errorf("expected empty errors for valid config, got: %v", errors)
	}
}

func TestMCPIntegration_Validate_InvalidConfig_ReturnsErrors(t *testing.T) {
	invalidCfg := `version: 1
strategies:
  - type: table
`
	tmpPath := filepath.Join(t.TempDir(), "invalid.yaml")
	if err := os.WriteFile(tmpPath, []byte(invalidCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	client := newMCPClient(t)
	resp := call(t, client, "bt_validate", map[string]any{
		"config_path": tmpPath,
	})

	if valid, ok := resp["valid"].(bool); !ok || valid {
		t.Error("expected valid=false for config missing target block")
	}
	errors, ok := resp["errors"].([]any)
	if !ok || len(errors) == 0 {
		t.Error("expected at least one error entry for invalid config")
	}
	for _, e := range errors {
		em := e.(map[string]any)
		if em["field"] == "" {
			t.Error("error entry has empty field")
		}
		if em["message"] == "" {
			t.Error("error entry has empty message")
		}
	}
}

func TestMCPIntegration_ScaffoldConfig_ProducesValidYAML(t *testing.T) {
	client := newMCPClient(t)
	outputPath := filepath.Join(t.TempDir(), "scaffolded.yaml")

	scaffoldResp := call(t, client, "bt_scaffold_config", map[string]any{
		"schema_path": ordersSchemaPath(t),
		"base_url":    "http://localhost:8080",
		"output_path": outputPath,
	})

	if written, ok := scaffoldResp["written_to_disk"].(bool); !ok || !written {
		t.Error("expected written_to_disk=true when output_path provided")
	}

	validateResp := call(t, client, "bt_validate", map[string]any{
		"config_path": outputPath,
	})
	if valid, ok := validateResp["valid"].(bool); !ok || !valid {
		t.Errorf("scaffolded config failed validation: %v", validateResp["errors"])
	}
}

func TestMCPIntegration_ScaffoldConfig_ResponseContainsConfigYAML(t *testing.T) {
	client := newMCPClient(t)
	resp := call(t, client, "bt_scaffold_config", map[string]any{
		"schema_path": ordersSchemaPath(t),
	})
	configYAML, ok := resp["config_yaml"].(string)
	if !ok || configYAML == "" {
		t.Error("expected non-empty config_yaml in scaffold response")
	}
}

func TestMCPIntegration_Run_TableStrategy_ReturnsStructuredSummary(t *testing.T) {
	requireOrdersAPI(t)
	client := newMCPClient(t)
	resp := call(t, client, "bt_run", map[string]any{
		"config_path": ordersConfigPath(t),
		"strategy":    "table",
	})

	for _, field := range []string{"passed", "failed", "total", "strategy", "duration_ms", "artifact_dir", "failures"} {
		if _, ok := resp[field]; !ok {
			t.Errorf("bt_run response missing required field %q", field)
		}
	}
}

func TestMCPIntegration_Run_PassedPlusFailedEqualsTotal(t *testing.T) {
	requireOrdersAPI(t)
	client := newMCPClient(t)
	resp := call(t, client, "bt_run", map[string]any{
		"config_path": ordersConfigPath(t),
		"strategy":    "table",
	})

	passed := int(resp["passed"].(float64))
	failed := int(resp["failed"].(float64))
	skipped := int(resp["skipped"].(float64))
	total := int(resp["total"].(float64))

	if passed+failed+skipped != total {
		t.Errorf("passed(%d)+failed(%d)+skipped(%d) != total(%d)", passed, failed, skipped, total)
	}
}

func TestMCPIntegration_Run_StrategyField_MatchesRequest(t *testing.T) {
	requireOrdersAPI(t)
	client := newMCPClient(t)
	resp := call(t, client, "bt_run", map[string]any{
		"config_path": ordersConfigPath(t),
		"strategy":    "table",
	})
	if resp["strategy"] != "table" {
		t.Errorf("expected strategy=table in response, got %v", resp["strategy"])
	}
}

func TestMCPIntegration_Run_DurationMs_IsPositive(t *testing.T) {
	requireOrdersAPI(t)
	client := newMCPClient(t)
	resp := call(t, client, "bt_run", map[string]any{
		"config_path": ordersConfigPath(t),
		"strategy":    "table",
	})
	durationMs := int(resp["duration_ms"].(float64))
	if durationMs <= 0 {
		t.Errorf("expected duration_ms > 0, got %d", durationMs)
	}
}

func TestMCPIntegration_Run_ArtifactDir_IsNonEmpty(t *testing.T) {
	requireOrdersAPI(t)
	client := newMCPClient(t)
	resp := call(t, client, "bt_run", map[string]any{
		"config_path": ordersConfigPath(t),
		"strategy":    "table",
	})
	if resp["artifact_dir"] == "" {
		t.Error("expected non-empty artifact_dir in bt_run response")
	}
}

func TestMCPIntegration_RunThenExplain_ArtifactPathChainWorks(t *testing.T) {
	requireOrdersAPI(t)
	client := newMCPClient(t)

	runResp := call(t, client, "bt_run", map[string]any{
		"config_path": ordersFailuresConfigPath(t),
		"strategy":    "table",
	})

	failures, ok := runResp["failures"].([]any)
	if !ok {
		t.Fatal("expected failures array in bt_run response")
	}
	if len(failures) == 0 {
		t.Skip("no failures produced — cannot test bt_explain_failure chain")
	}

	firstFailure := failures[0].(map[string]any)
	artifactPath, ok := firstFailure["artifact_path"].(string)
	if !ok || artifactPath == "" {
		t.Fatal("first failure has no artifact_path")
	}

	explainResp := call(t, client, "bt_explain_failure", map[string]any{
		"artifact_path": artifactPath,
	})

	for _, field := range []string{"case_id", "strategy", "request", "response", "failures", "replay_command"} {
		if _, ok := explainResp[field]; !ok {
			t.Errorf("bt_explain_failure response missing field %q", field)
		}
	}
}

func TestMCPIntegration_ExplainFailure_ReplayCommand_ContainsBtReplay(t *testing.T) {
	requireOrdersAPI(t)
	client := newMCPClient(t)

	runResp := call(t, client, "bt_run", map[string]any{
		"config_path": ordersFailuresConfigPath(t),
		"strategy":    "table",
	})
	failures := runResp["failures"].([]any)
	if len(failures) == 0 {
		t.Skip("no failures to explain")
	}
	artifactPath := failures[0].(map[string]any)["artifact_path"].(string)

	explainResp := call(t, client, "bt_explain_failure", map[string]any{
		"artifact_path": artifactPath,
	})

	replayCmd, ok := explainResp["replay_command"].(string)
	if !ok || replayCmd == "" {
		t.Error("expected non-empty replay_command")
	}
	if !strings.Contains(replayCmd, "bt replay") {
		t.Errorf("replay_command %q should contain 'bt replay'", replayCmd)
	}
	if !strings.Contains(replayCmd, artifactPath) {
		t.Errorf("replay_command %q should contain artifact path %q", replayCmd, artifactPath)
	}
}

func TestMCPIntegration_ExplainFailure_RequestAndResponse_ArePopulated(t *testing.T) {
	requireOrdersAPI(t)
	client := newMCPClient(t)

	runResp := call(t, client, "bt_run", map[string]any{
		"config_path": ordersFailuresConfigPath(t),
		"strategy":    "table",
	})
	failures := runResp["failures"].([]any)
	if len(failures) == 0 {
		t.Skip("no failures to explain")
	}
	artifactPath := failures[0].(map[string]any)["artifact_path"].(string)

	explainResp := call(t, client, "bt_explain_failure", map[string]any{
		"artifact_path": artifactPath,
	})

	req, ok := explainResp["request"].(map[string]any)
	if !ok {
		t.Fatal("expected request object in explain response")
	}
	if req["method"] == "" {
		t.Error("request.method must not be empty")
	}
	if req["url"] == "" {
		t.Error("request.url must not be empty")
	}

	resp, ok := explainResp["response"].(map[string]any)
	if !ok {
		t.Fatal("expected response object in explain response")
	}
	if resp["status_code"] == nil {
		t.Error("response.status_code must be present")
	}
}
