package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/cli"
	"github.com/jayimbery/bt/pkg/model"
)

func writeTestArtifact(t *testing.T, dir string, statusCode int) string {
	t.Helper()

	a := model.Artifact{
		ID:           "artifact-001",
		StrategyKind: "table",
		CaseID:       "create-order-valid",
		OccurredAt:   time.Now().UTC(),
		Environment:  "test",
		Request: model.RequestDetail{
			Method:  "POST",
			URL:     "/orders",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"amount":100,"currency":"GBP"}`),
		},
		Response: model.ResponseDetail{
			StatusCode: statusCode,
			Body:       []byte(`{"error":"internal server error"}`),
		},
		Failures: []model.Failure{
			{
				Invariant: "status_code",
				Message:   "expected 201, got 500",
				Expected:  201,
				Actual:    statusCode,
			},
		},
	}

	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "2026-05-09T143022Z-create-order-valid.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	return path
}

func writeReplayConfig(t *testing.T, dir, baseURL string) string {
	t.Helper()
	yaml := "version: 1\ntarget:\n  name: test-api\n  base_url: " + baseURL +
		"\n  schema: ./openapi.yaml\n"
	path := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplayCommand_ArtifactNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfgPath := writeReplayConfig(t, dir, "http://localhost:8080")

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"replay", "--config", cfgPath, "/nonexistent/artifact.json"})
	cmd.SetOut(&bytes.Buffer{})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for missing artifact file")
	}
}

func TestReplayCommand_FailureStillPresent(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	artifactPath := writeTestArtifact(t, dir, 500)
	cfgPath := writeReplayConfig(t, dir, server.URL)

	var out bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"replay", "--config", cfgPath, artifactPath})
	cmd.SetOut(&out)

	_ = cmd.Execute()

	output := out.String()
	if !containsString(output, "still present") && !containsString(output, "FAIL") {
		t.Errorf("expected output to indicate failure is still present, got:\n%s", output)
	}
}

func TestReplayCommand_FailureNoLongerReproducible(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	dir := t.TempDir()
	artifactPath := writeTestArtifact(t, dir, 500)
	cfgPath := writeReplayConfig(t, dir, server.URL)

	var out bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"replay", "--config", cfgPath, artifactPath})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Errorf("expected replay to succeed when failure not reproducible, got: %v", err)
	}

	output := out.String()
	if !containsString(output, "no longer") && !containsString(output, "PASS") {
		t.Errorf("expected output to indicate failure is no longer reproducible, got:\n%s", output)
	}
}

func containsString(s, substr string) bool {
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
