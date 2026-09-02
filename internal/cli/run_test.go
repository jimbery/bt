package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/jimbery/bt/internal/cli"
)

func TestRunCommand_TableStrategy_AllPass(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()

	spec := `
openapi: "3.0.3"
info:
  title: Test API
  version: "1.0.0"
paths:
  /health:
    get:
      operationId: GetHealth
      responses:
        "200":
          description: OK
`
	specPath := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := `
cases:
  - id: health-check
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
`
	casesPath := filepath.Join(dir, "cases.yaml")
	if err := os.WriteFile(casesPath, []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := "version: 1\ntarget:\n  name: test-api\n  base_url: " + server.URL +
		"\n  schema: " + specPath + "\nstrategies:\n  - type: table\n    file: " + casesPath + "\n"
	cfgPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"run", "--config", cfgPath, "--strategy", "table"})
	cmd.SetOut(&bytes.Buffer{})

	err := cmd.Execute()
	if err != nil {
		t.Errorf("expected run to succeed with all passing cases, got: %v", err)
	}
}

func TestRunCommand_TableStrategy_FailuresReturnError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()

	spec := `
openapi: "3.0.3"
info:
  title: Test API
  version: "1.0.0"
paths:
  /health:
    get:
      operationId: GetHealth
      responses:
        "200":
          description: OK
`
	specPath := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(specPath, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := `
cases:
  - id: health-check
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
`
	casesPath := filepath.Join(dir, "cases.yaml")
	if err := os.WriteFile(casesPath, []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := "version: 1\ntarget:\n  name: test-api\n  base_url: " + server.URL +
		"\n  schema: " + specPath + "\nstrategies:\n  - type: table\n    file: " + casesPath + "\n"
	cfgPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"run", "--config", cfgPath, "--strategy", "table"})
	cmd.SetOut(&bytes.Buffer{})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected run to return an error when cases fail")
	}
}
