package doctor

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctor_SchemaFileExists_SchemaReachablePasses(t *testing.T) {
	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(schemaPath, []byte("openapi: 3.0.0"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := CheckSchemaReachable(schemaPath)

	if !result.Passed {
		t.Errorf("expected check to pass, got: %s", result.Message)
	}
	if result.ID != "schema-reachable" {
		t.Errorf("expected ID 'schema-reachable', got %q", result.ID)
	}
	if result.Message == "" {
		t.Error("Message must be non-empty")
	}
}

func TestDoctor_SchemaFileMissing_SchemaReachableFails(t *testing.T) {
	result := CheckSchemaReachable("/no/such/schema.yaml")

	if result.Passed {
		t.Error("expected check to fail for missing schema file")
	}
	if result.Message == "" {
		t.Error("Message must describe what was checked")
	}
}

func TestDoctor_TargetReturns200_TargetReachablePasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	result := CheckTargetReachable(srv.URL)

	if !result.Passed {
		t.Errorf("expected check to pass, got: %s", result.Message)
	}
	if result.DurationMs < 0 {
		t.Error("DurationMs must not be negative")
	}
	if !strings.Contains(result.Message, "ms") {
		t.Errorf("expected message to report duration in ms, got %q", result.Message)
	}
}

func TestDoctor_TargetUnreachable_TargetReachableFails(t *testing.T) {
	result := CheckTargetReachable("http://127.0.0.1:59999/health")

	if result.Passed {
		t.Error("expected check to fail for unreachable target")
	}
}

func TestDoctor_TargetReturnsNon200_TargetReachableFailsWithStatusInMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	result := CheckTargetReachable(srv.URL)

	if result.Passed {
		t.Error("expected check to fail for non-200 response")
	}
	if result.Message == "" {
		t.Error("Message must report what status was received")
	}
}

func TestDoctor_AuthEnvVarSet_AuthConfiguredPasses(t *testing.T) {
	t.Setenv("BT_AUTH_TOKEN", "secret")

	result := CheckAuthConfigured("BT_AUTH_TOKEN")

	if !result.Passed {
		t.Errorf("expected check to pass when env var is set, got: %s", result.Message)
	}
}

func TestDoctor_AuthEnvVarEmpty_AuthConfiguredFails(t *testing.T) {
	t.Setenv("BT_AUTH_TOKEN", "")

	result := CheckAuthConfigured("BT_AUTH_TOKEN")

	if result.Passed {
		t.Error("expected check to fail when env var is empty")
	}
	if result.Message == "" {
		t.Error("Message must name the missing env var")
	}
}

func TestDoctor_AuthEnvVarNotRequired_AuthConfiguredReturnsWarn(t *testing.T) {
	result := CheckAuthConfigured("")

	if result.Level != Warn {
		t.Errorf("expected Warn level when auth not required, got %v", result.Level)
	}
}

func TestDoctor_ValidBaselineFile_BaselineValidPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	content := "version: 1\nquarantined:\n  - operation_id: GetOrderBroken\n    reason: \"test\"\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result := CheckBaselineValid(path)

	if !result.Passed {
		t.Errorf("expected check to pass for valid baseline, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "quarantined") {
		t.Errorf("expected message to mention quarantine, got %q", result.Message)
	}
}

func TestDoctor_MalformedBaselineFile_BaselineValidFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "baseline.yaml")
	if err := os.WriteFile(path, []byte("not: valid: yaml: ["), 0o644); err != nil {
		t.Fatal(err)
	}

	result := CheckBaselineValid(path)

	if result.Passed {
		t.Error("expected check to fail for malformed baseline")
	}
}

func TestDoctor_BaselineFileMissing_BaselineValidReturnsWarn(t *testing.T) {
	result := CheckBaselineValid("/no/such/baseline.yaml")

	if result.Level != Warn {
		t.Errorf("expected Warn for missing baseline file, got %v", result.Level)
	}
}

func TestDoctor_RunAll_ReturnsOneResultPerCheck(t *testing.T) {
	cfg := Config{
		SchemaPath:    "/no/such/schema.yaml",
		TargetBaseURL: "http://127.0.0.1:59999",
		AuthEnvVar:    "",
		BaselinePath:  "/no/such/baseline.yaml",
	}

	results := RunAll(cfg)

	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
	for _, r := range results {
		if r.ID == "" {
			t.Error("every CheckResult must have a non-empty ID")
		}
		if r.Message == "" {
			t.Error("every CheckResult must have a non-empty Message")
		}
	}
}
