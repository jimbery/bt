package cli_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jayimbery/bt/internal/cli"
)

func writeMinimalOpenAPISpec(t *testing.T, dir string) string {
	t.Helper()
	spec := `openapi: "3.0.3"
info:
  title: Orders API
  version: "1.0.0"
paths:
  /orders:
    post:
      operationId: CreateOrder
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              properties:
                amount:
                  type: integer
                currency:
                  type: string
              required: [amount, currency]
      responses:
        "201":
          description: created
`
	path := filepath.Join(dir, "openapi.yaml")
	if err := os.WriteFile(path, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeMinimalCases(t *testing.T, dir string) string {
	t.Helper()
	cases := `cases:
  - id: smoke
    operation_id: CreateOrder
    input:
      method: POST
      path: /orders
      body:
        amount: 1
        currency: GBP
    expected:
      status_code: 201
`
	path := filepath.Join(dir, "cases.yaml")
	if err := os.WriteFile(path, []byte(cases), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTraceTestConfig(t *testing.T, dir, specPath, casesPath, profileRel string) string {
	t.Helper()
	traceLine := ""
	if profileRel != "" {
		traceLine = fmt.Sprintf("trace:\n  profile: %s\n", profileRel)
	}
	cfg := fmt.Sprintf(`version: 1
target:
  name: orders-api
  base_url: http://127.0.0.1:9
  schema: %s
  adapter: openapi
%sstrategies:
  - type: table
    file: %s
`, specPath, traceLine, casesPath)
	path := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func harEntryJSON(started, method, url, reqJSON string, status int, respJSON string) string {
	return fmt.Sprintf(`{
        "startedDateTime": %q,
        "time": 1.0,
        "request": {
          "method": %q,
          "url": %q,
          "headers": [],
          "postData": {
            "mimeType": "application/json",
            "text": %q
          }
        },
        "response": {
          "status": %d,
          "headers": [],
          "content": {
            "mimeType": "application/json",
            "text": %q
          }
        }
      }`, started, method, url, reqJSON, status, respJSON)
}

func writeOrdersHAR(t *testing.T, path string, n int) {
	t.Helper()
	var parts []string
	for i := 0; i < n; i++ {
		cur := "GBP"
		switch i % 3 {
		case 1:
			cur = "USD"
		case 2:
			cur = "EUR"
		}
		req := fmt.Sprintf(`{"amount":%d,"currency":"%s"}`, 100+i, cur)
		resp := `{"id":"x","status":"pending"}`
		parts = append(parts, harEntryJSON(
			fmt.Sprintf("2024-01-15T10:00:%02dZ", i%60),
			"POST",
			"https://api.example.com/orders",
			req,
			201,
			resp,
		))
	}
	doc := fmt.Sprintf(`{"log":{"version":"1.2","entries":[%s]}}`, strings.Join(parts, ","))
	if err := os.WriteFile(path, []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTraceImport_ValidHAR_WritesProfile(t *testing.T) {
	dir := t.TempDir()
	harPath := filepath.Join(dir, "sample.har")
	profilePath := filepath.Join(dir, ".bt", "trace", "profile.json")
	specPath := writeMinimalOpenAPISpec(t, dir)
	casesPath := writeMinimalCases(t, dir)
	cfgPath := writeTraceTestConfig(t, dir, specPath, casesPath, ".bt/trace/profile.json")

	writeOrdersHAR(t, harPath, 30)

	var out bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--config", cfgPath, "trace", "import", harPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("trace import: %v", err)
	}

	if _, err := os.Stat(profilePath); err != nil {
		t.Fatalf("expected profile at %q: %v", profilePath, err)
	}

	data, err := os.ReadFile(profilePath)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatal("profile is not valid JSON")
	}

	outStr := out.String()
	if !strings.Contains(outStr, "operation") {
		t.Errorf("expected output to mention operations; got: %s", outStr)
	}
	if !strings.Contains(outStr, ".bt/trace/profile.json") && !strings.Contains(outStr, profilePath) {
		t.Errorf("expected output to mention profile path; got: %s", outStr)
	}
}

func TestTraceImport_MissingHARFile_ExitsWithError(t *testing.T) {
	dir := t.TempDir()
	specPath := writeMinimalOpenAPISpec(t, dir)
	casesPath := writeMinimalCases(t, dir)
	cfgPath := writeTraceTestConfig(t, dir, specPath, casesPath, "")

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "trace", "import", "/nonexistent/file.har"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing HAR file")
	}
}

func TestTraceImport_MalformedHAR_ExitsWithError(t *testing.T) {
	dir := t.TempDir()
	harPath := filepath.Join(dir, "bad.har")
	if err := os.WriteFile(harPath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	specPath := writeMinimalOpenAPISpec(t, dir)
	casesPath := writeMinimalCases(t, dir)
	cfgPath := writeTraceTestConfig(t, dir, specPath, casesPath, "")

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"--config", cfgPath, "trace", "import", harPath})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for malformed HAR")
	}
}

func TestTraceInspect_WritesSummary(t *testing.T) {
	dir := t.TempDir()
	harPath := filepath.Join(dir, "sample.har")
	specPath := writeMinimalOpenAPISpec(t, dir)
	casesPath := writeMinimalCases(t, dir)
	cfgPath := writeTraceTestConfig(t, dir, specPath, casesPath, ".bt/trace/profile.json")
	writeOrdersHAR(t, harPath, 5)

	importCmd := cli.NewRootCmd()
	importCmd.SetArgs([]string{"--config", cfgPath, "trace", "import", harPath})
	if err := importCmd.Execute(); err != nil {
		t.Fatalf("import: %v", err)
	}

	var out bytes.Buffer
	inspectCmd := cli.NewRootCmd()
	inspectCmd.SetOut(&out)
	inspectCmd.SetArgs([]string{"--config", cfgPath, "trace", "inspect"})
	if err := inspectCmd.Execute(); err != nil {
		t.Fatalf("trace inspect: %v", err)
	}
	s := out.String()
	if !strings.Contains(s, "CreateOrder") {
		t.Fatalf("expected inspect to mention CreateOrder; got:\n%s", s)
	}
}
