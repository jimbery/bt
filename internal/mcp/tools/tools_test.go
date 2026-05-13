package tools_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jayimbery/bt/internal/mcp/tools"
	"github.com/jayimbery/bt/pkg/model"
)

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("cannot marshal: %v", err)
	}
	return b
}

func mustUnmarshal(t *testing.T, data json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("cannot unmarshal response: %v\nraw: %s", err, data)
	}
}

func sampleArtifact() model.Artifact {
	return model.Artifact{
		ID:           "test-artifact",
		CaseID:       "CreateOrder",
		StrategyKind: "table",
		Request: model.RequestDetail{
			Method:  "POST",
			URL:     "http://localhost:8080/orders",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"amount":99.99,"currency":"GBP"}`),
		},
		Response: model.ResponseDetail{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       []byte(`{"error":"internal server error","code":"INTERNAL"}`),
		},
		Failures: []model.Failure{
			{Invariant: "no_5xx", Message: "expected < 500, got 500", Expected: "< 500", Actual: "500"},
		},
	}
}

func TestDiscoverOperations_MissingSchemaPath_ReturnsValidationError(t *testing.T) {
	h := tools.DiscoverOperationsHandler()
	input := mustMarshal(t, map[string]any{})
	_, err := h(context.Background(), input)
	if err == nil {
		t.Error("expected error for missing required schema_path")
	}
}

func TestDiscoverOperations_NonexistentFile_ReturnsStructuredError(t *testing.T) {
	h := tools.DiscoverOperationsHandler()
	input := mustMarshal(t, map[string]any{
		"schema_path": "/does/not/exist/openapi.yaml",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured error response, not Go error: %v", err)
	}
	var resp map[string]any
	mustUnmarshal(t, result, &resp)
	if _, hasErr := resp["error"]; !hasErr {
		t.Error("expected 'error' field in response for missing file")
	}
	if resp["code"] != "SCHEMA_PARSE_ERROR" {
		t.Errorf("expected code SCHEMA_PARSE_ERROR, got %v", resp["code"])
	}
}

func TestDiscoverOperations_ValidSchema_ReturnsOperations(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.DiscoverOperationsHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Operations     []map[string]any `json:"operations"`
		OperationCount int              `json:"operation_count"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.OperationCount == 0 {
		t.Error("expected at least one operation from a valid schema")
	}
	if len(resp.Operations) != resp.OperationCount {
		t.Errorf("OperationCount=%d but Operations has %d entries", resp.OperationCount, len(resp.Operations))
	}
}

func TestDiscoverOperations_EachOperation_HasRequiredFields(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.DiscoverOperationsHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		Operations []map[string]any `json:"operations"`
	}
	mustUnmarshal(t, result, &resp)
	for _, op := range resp.Operations {
		for _, field := range []string{"id", "method", "path"} {
			if _, ok := op[field]; !ok {
				t.Errorf("operation missing required field %q: %v", field, op)
			}
		}
	}
}

func TestSuggestStrategy_EmptyOperations_ReturnsEmptyRecommendations(t *testing.T) {
	h := tools.SuggestStrategyHandler(nil)
	input := mustMarshal(t, map[string]any{"operations": []any{}})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Recommendations []any `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	if len(resp.Recommendations) != 0 {
		t.Errorf("expected empty recommendations for empty operations, got %d", len(resp.Recommendations))
	}
}

func TestSuggestStrategy_POSTWithBody_RecommendsPropertyAndFuzz(t *testing.T) {
	h := tools.SuggestStrategyHandler(nil)
	input := mustMarshal(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Recommendations []struct {
			OperationID string `json:"operation_id"`
			Strategies  []struct {
				Strategy string `json:"strategy"`
				Priority string `json:"priority"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	if len(resp.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(resp.Recommendations))
	}
	rec := resp.Recommendations[0]
	if rec.OperationID != "CreateOrder" {
		t.Errorf("expected OperationID=CreateOrder, got %q", rec.OperationID)
	}
	recommended := map[string]bool{}
	for _, s := range rec.Strategies {
		if s.Priority == "recommended" {
			recommended[s.Strategy] = true
		}
	}
	for _, expected := range []string{"table", "property", "fuzz"} {
		if !recommended[expected] {
			t.Errorf("expected %q to be 'recommended' for POST with body", expected)
		}
	}
}

func TestSuggestStrategy_GETNoBody_DoesNotRecommendPropertyAsPrimary(t *testing.T) {
	h := tools.SuggestStrategyHandler(nil)
	input := mustMarshal(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "GetOrder", "method": "GET", "has_body": false},
		},
	})
	result, _ := h(context.Background(), input)
	var resp struct {
		Recommendations []struct {
			Strategies []struct {
				Strategy string `json:"strategy"`
				Priority string `json:"priority"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	for _, s := range resp.Recommendations[0].Strategies {
		if s.Strategy == "property" && s.Priority == "recommended" {
			t.Error("property should not be 'recommended' (only 'optional') for GET with no body")
		}
	}
}

func TestSuggestStrategy_AllOperations_HaveTableRecommended(t *testing.T) {
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	h := tools.SuggestStrategyHandler(nil)
	for _, method := range methods {
		input := mustMarshal(t, map[string]any{
			"operations": []any{
				map[string]any{"id": "Op", "method": method, "has_body": method == "POST"},
			},
		})
		result, _ := h(context.Background(), input)
		var resp struct {
			Recommendations []struct {
				Strategies []struct {
					Strategy string `json:"strategy"`
					Priority string `json:"priority"`
				} `json:"strategies"`
			} `json:"recommendations"`
		}
		mustUnmarshal(t, result, &resp)
		tableRecommended := false
		for _, s := range resp.Recommendations[0].Strategies {
			if s.Strategy == "table" && s.Priority == "recommended" {
				tableRecommended = true
			}
		}
		if !tableRecommended {
			t.Errorf("table should be recommended for %s operation", method)
		}
	}
}

func TestSuggestStrategy_EachRecommendation_HasRationale(t *testing.T) {
	h := tools.SuggestStrategyHandler(nil)
	input := mustMarshal(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})
	result, _ := h(context.Background(), input)
	var resp struct {
		Recommendations []struct {
			Strategies []struct {
				Strategy  string `json:"strategy"`
				Rationale string `json:"rationale"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	for _, s := range resp.Recommendations[0].Strategies {
		if s.Rationale == "" {
			t.Errorf("strategy %q has empty rationale", s.Strategy)
		}
	}
}

func TestSuggestStrategy_WithTraceProfile_SuggestsStateful(t *testing.T) {
	h := tools.SuggestStrategyHandler(nil)
	input := mustMarshal(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
		"trace_profile": map[string]any{
			"schema_version": "1",
			"operations":     map[string]any{},
			"sequences": map[string]any{
				"start_probability": map[string]any{"CreateOrder": 0.85, "ListOrders": 0.15},
				"transitions": map[string]any{
					"CreateOrder": map[string]any{"GetOrder": 0.87, "__END__": 0.13},
				},
			},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Recommendations []struct {
			Strategies []struct {
				Strategy string `json:"strategy"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	found := false
	for _, s := range resp.Recommendations[0].Strategies {
		if s.Strategy == "stateful" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected stateful strategy in recommendations when trace_profile has multiple start operations")
	}
}

func TestValidate_ValidConfig_ReturnsValidTrue(t *testing.T) {
	configPath := writeMinimalConfig(t)
	h := tools.ValidateHandler()
	input := mustMarshal(t, map[string]any{"config_path": configPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Valid  bool  `json:"valid"`
		Errors []any `json:"errors"`
	}
	mustUnmarshal(t, result, &resp)
	if !resp.Valid {
		t.Errorf("expected valid=true for a valid config, errors: %v", resp.Errors)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("expected empty errors for valid config, got: %v", resp.Errors)
	}
}

func TestValidate_InvalidConfig_ReturnsValidFalseWithErrors(t *testing.T) {
	configPath := writeInvalidConfig(t)
	h := tools.ValidateHandler()
	input := mustMarshal(t, map[string]any{"config_path": configPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Valid  bool `json:"valid"`
		Errors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.Valid {
		t.Error("expected valid=false for invalid config")
	}
	if len(resp.Errors) == 0 {
		t.Error("expected at least one error entry for invalid config")
	}
}

func TestValidate_ErrorEntry_HasFieldAndMessage(t *testing.T) {
	configPath := writeInvalidConfig(t)
	h := tools.ValidateHandler()
	input := mustMarshal(t, map[string]any{"config_path": configPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		Errors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	mustUnmarshal(t, result, &resp)
	for _, e := range resp.Errors {
		if e.Field == "" {
			t.Error("error entry has empty field")
		}
		if e.Message == "" {
			t.Error("error entry has empty message")
		}
	}
}

func TestValidate_MissingFile_ReturnsValidFalse(t *testing.T) {
	h := tools.ValidateHandler()
	input := mustMarshal(t, map[string]any{"config_path": "/does/not/exist.yaml"})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured response, not Go error: %v", err)
	}
	var resp struct {
		Valid bool `json:"valid"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.Valid {
		t.Error("expected valid=false for missing config file")
	}
}

func TestScaffoldConfig_ValidSchema_ReturnsYAML(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		ConfigYAML    string `json:"config_yaml"`
		WrittenToDisk bool   `json:"written_to_disk"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.ConfigYAML == "" {
		t.Error("expected non-empty config_yaml in response")
	}
	if resp.WrittenToDisk {
		t.Error("expected written_to_disk=false when no output_path given")
	}
}

func TestScaffoldConfig_GeneratedYAML_ContainsVersion(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		ConfigYAML string `json:"config_yaml"`
	}
	mustUnmarshal(t, result, &resp)
	if !containsString(resp.ConfigYAML, "version: 1") {
		t.Error("generated config must contain 'version: 1'")
	}
}

func TestScaffoldConfig_GeneratedYAML_ContainsTargetBlock(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath, "base_url": "http://localhost:9000"})
	result, _ := h(context.Background(), input)
	var resp struct {
		ConfigYAML string `json:"config_yaml"`
	}
	mustUnmarshal(t, result, &resp)
	if !containsString(resp.ConfigYAML, "base_url") {
		t.Error("generated config must contain base_url")
	}
	if !containsString(resp.ConfigYAML, "http://localhost:9000") {
		t.Error("generated config must use the provided base_url")
	}
}

func TestScaffoldConfig_WithOutputPath_WritesFileToDisk(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	outputPath := filepath.Join(t.TempDir(), "backendtest.yaml")
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{
		"schema_path": schemaPath,
		"output_path": outputPath,
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		WrittenToDisk bool `json:"written_to_disk"`
	}
	mustUnmarshal(t, result, &resp)
	if !resp.WrittenToDisk {
		t.Error("expected written_to_disk=true when output_path is provided")
	}
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("expected file to exist at output_path after scaffold")
	}
}

func TestScaffoldConfig_GeneratedYAML_IsParseableByConfigLoader(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	outputPath := filepath.Join(t.TempDir(), "backendtest.yaml")
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{
		"schema_path": schemaPath,
		"output_path": outputPath,
	})
	_, _ = h(context.Background(), input)

	vh := tools.ValidateHandler()
	vInput := mustMarshal(t, map[string]any{"config_path": outputPath})
	vResult, _ := vh(context.Background(), vInput)
	var vResp struct {
		Valid  bool  `json:"valid"`
		Errors []any `json:"errors"`
	}
	mustUnmarshal(t, vResult, &vResp)
	if !vResp.Valid {
		t.Errorf("generated config failed validation: %v", vResp.Errors)
	}
}

func TestRun_MissingConfigPath_ReturnsValidationError(t *testing.T) {
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{})
	_, err := h(context.Background(), input)
	if err == nil {
		t.Error("expected error for missing config_path")
	}
}

func TestRun_InvalidConfigPath_ReturnsStructuredError(t *testing.T) {
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{"config_path": "/no/such/config.yaml"})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured error response, not Go error: %v", err)
	}
	var resp map[string]any
	mustUnmarshal(t, result, &resp)
	if _, hasErr := resp["error"]; !hasErr {
		t.Error("expected 'error' field for invalid config path")
	}
}

func TestRun_SuccessfulRun_ResponseHasRequiredFields(t *testing.T) {
	configPath := writeRunnableConfig(t)
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{
		"config_path": configPath,
		"strategy":    "table",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Passed      int    `json:"passed"`
		Failed      int    `json:"failed"`
		Skipped     int    `json:"skipped"`
		Total       int    `json:"total"`
		Strategy    string `json:"strategy"`
		DurationMs  int    `json:"duration_ms"`
		ArtifactDir string `json:"artifact_dir"`
		Failures    []any  `json:"failures"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.Strategy == "" {
		t.Error("response must include strategy field")
	}
	if resp.Total == 0 {
		t.Error("expected total > 0 for a run with at least one case")
	}
	if resp.ArtifactDir == "" {
		t.Error("response must include artifact_dir")
	}
	if resp.Failures == nil {
		t.Error("failures field must be present (even if empty)")
	}
}

func TestRun_FailedCase_IncludesArtifactPath(t *testing.T) {
	configPath := writeFailingConfig(t)
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{
		"config_path": configPath,
		"strategy":    "table",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Failures []struct {
			CaseID       string `json:"case_id"`
			ArtifactPath string `json:"artifact_path"`
			Summary      string `json:"summary"`
		} `json:"failures"`
	}
	mustUnmarshal(t, result, &resp)
	if len(resp.Failures) == 0 {
		t.Fatal("expected at least one failure from a failing config")
	}
	for _, f := range resp.Failures {
		if f.ArtifactPath == "" {
			t.Errorf("failure %q has no artifact_path", f.CaseID)
		}
		if f.Summary == "" {
			t.Errorf("failure %q has no summary", f.CaseID)
		}
	}
}

func TestRun_PassedCount_PlusFailedCount_EqualsTotal(t *testing.T) {
	configPath := writeRunnableConfig(t)
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{"config_path": configPath, "strategy": "table"})
	result, _ := h(context.Background(), input)
	var resp struct {
		Passed  int `json:"passed"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
		Total   int `json:"total"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.Passed+resp.Failed+resp.Skipped != resp.Total {
		t.Errorf("passed(%d)+failed(%d)+skipped(%d) != total(%d)",
			resp.Passed, resp.Failed, resp.Skipped, resp.Total)
	}
}

func TestExplainFailure_ValidArtifact_ReturnsStructuredDetail(t *testing.T) {
	artifactPath := writeArtifactFile(t, sampleArtifact())
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": artifactPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		CaseID   string `json:"case_id"`
		Strategy string `json:"strategy"`
		Request  struct {
			Method string `json:"method"`
			URL    string `json:"url"`
		} `json:"request"`
		Response struct {
			StatusCode int `json:"status_code"`
		} `json:"response"`
		Failures []struct {
			Invariant string `json:"invariant"`
			Message   string `json:"message"`
		} `json:"failures"`
		ReplayCommand string `json:"replay_command"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.CaseID != "CreateOrder" {
		t.Errorf("expected CaseID=CreateOrder, got %q", resp.CaseID)
	}
	if resp.Request.Method != "POST" {
		t.Errorf("expected request method POST, got %q", resp.Request.Method)
	}
	if resp.Response.StatusCode != 500 {
		t.Errorf("expected status code 500, got %d", resp.Response.StatusCode)
	}
	if len(resp.Failures) == 0 {
		t.Error("expected at least one failure in artifact detail")
	}
	if resp.ReplayCommand == "" {
		t.Error("expected replay_command to be set")
	}
}

func TestExplainFailure_ReplayCommand_ContainsBtReplay(t *testing.T) {
	artifactPath := writeArtifactFile(t, sampleArtifact())
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": artifactPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		ReplayCommand string `json:"replay_command"`
	}
	mustUnmarshal(t, result, &resp)
	if !containsString(resp.ReplayCommand, "bt replay") {
		t.Errorf("expected replay_command to contain 'bt replay', got %q", resp.ReplayCommand)
	}
}

func TestExplainFailure_ReplayCommand_ContainsArtifactPath(t *testing.T) {
	artifactPath := writeArtifactFile(t, sampleArtifact())
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": artifactPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		ReplayCommand string `json:"replay_command"`
	}
	mustUnmarshal(t, result, &resp)
	if !containsString(resp.ReplayCommand, artifactPath) {
		t.Errorf("expected replay_command to contain artifact path %q, got %q", artifactPath, resp.ReplayCommand)
	}
}

func TestExplainFailure_MissingArtifact_ReturnsStructuredError(t *testing.T) {
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": "/does/not/exist.json"})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured error response, not Go error: %v", err)
	}
	var resp map[string]any
	mustUnmarshal(t, result, &resp)
	if resp["code"] != "ARTIFACT_NOT_FOUND" {
		t.Errorf("expected code ARTIFACT_NOT_FOUND, got %v", resp["code"])
	}
}

func TestExplainFailure_FailureEntries_HaveInvariantAndMessage(t *testing.T) {
	artifactPath := writeArtifactFile(t, sampleArtifact())
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": artifactPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		Failures []struct {
			Invariant string `json:"invariant"`
			Message   string `json:"message"`
		} `json:"failures"`
	}
	mustUnmarshal(t, result, &resp)
	for _, f := range resp.Failures {
		if f.Invariant == "" {
			t.Error("failure entry has empty invariant")
		}
		if f.Message == "" {
			t.Error("failure entry has empty message")
		}
	}
}

func writeMinimalOpenAPISpec(t *testing.T) string {
	t.Helper()
	spec := `openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /health:
    get:
      operationId: GetHealth
      responses:
        "200":
          description: OK
  /orders:
    post:
      operationId: CreateOrder
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [amount, currency]
              properties:
                amount:
                  type: number
                currency:
                  type: string
      responses:
        "201":
          description: Created
`
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatalf("cannot write openapi spec: %v", err)
	}
	return path
}

func writeMinimalConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	schemaPath := writeMinimalOpenAPISpecInDir(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "cases"), 0o755); err != nil {
		t.Fatal(err)
	}
	tablePath := filepath.Join(dir, "cases", "table.yaml")
	if err := os.WriteFile(tablePath, []byte("cases: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`version: 1
target:
  name: test-api
  base_url: http://localhost:8080
  schema: %s
strategies:
  - type: table
    file: %s
safety:
  profile: safe
`, schemaPath, tablePath)
	path := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeInvalidConfig(t *testing.T) string {
	t.Helper()
	cfg := `version: 1
strategies:
  - type: table
`
	path := filepath.Join(t.TempDir(), "backendtest-invalid.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeArtifactFile(t *testing.T, a model.Artifact) string {
	t.Helper()
	dir := t.TempDir()
	data, _ := json.MarshalIndent(a, "", "  ")
	path := filepath.Join(dir, "artifact.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		func() bool {
			for i := 0; i <= len(haystack)-len(needle); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}())
}

func writeRunnableConfig(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	schemaPath := writeMinimalOpenAPISpecInDir(t, dir)
	_ = os.MkdirAll(filepath.Join(dir, "cases"), 0o755)
	tablePath := filepath.Join(dir, "cases", "table.yaml")
	table := `cases:
  - id: h
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
`
	if err := os.WriteFile(tablePath, []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`version: 1
target:
  name: test-api
  base_url: %s
  schema: %s
strategies:
  - type: table
    file: %s
safety:
  profile: safe
`, srv.URL, schemaPath, tablePath)
	cfgPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func writeFailingConfig(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	schemaPath := writeMinimalOpenAPISpecInDir(t, dir)
	_ = os.MkdirAll(filepath.Join(dir, "cases"), 0o755)
	tablePath := filepath.Join(dir, "cases", "table.yaml")
	table := `cases:
  - id: h
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
`
	if err := os.WriteFile(tablePath, []byte(table), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := fmt.Sprintf(`version: 1
target:
  name: test-api
  base_url: %s
  schema: %s
strategies:
  - type: table
    file: %s
safety:
  profile: safe
`, srv.URL, schemaPath, tablePath)
	cfgPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgPath
}

func writeMinimalOpenAPISpecInDir(t *testing.T, dir string) string {
	t.Helper()
	spec := `openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /health:
    get:
      operationId: GetHealth
      responses:
        "200":
          description: OK
  /orders:
    post:
      operationId: CreateOrder
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [amount, currency]
              properties:
                amount:
                  type: number
                currency:
                  type: string
      responses:
        "201":
          description: Created
`
	path := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatalf("cannot write openapi spec: %v", err)
	}
	return path
}
