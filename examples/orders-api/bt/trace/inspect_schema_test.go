//go:build integration

package trace_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/cli"
	"github.com/jimbery/bt/internal/testutil"
)

func TestTraceInspect_JSONShape(t *testing.T) {
	root := testutil.RepoRoot(t)
	harAbs := filepath.Join(root, "examples/orders-api/bt/trace/sample.har")
	specAbs := filepath.Join(root, "examples/orders-api/spec/openapi.yaml")
	casesAbs := filepath.Join(root, "examples/orders-api/bt/cases/table.yaml")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "backendtest.yaml")
	cfg := `version: 1
target:
  name: orders-api
  base_url: http://127.0.0.1:9
  schema: ` + specAbs + `
  adapter: openapi
strategies:
  - type: table
    file: ` + casesAbs + `
trace:
  profile: .bt/trace/profile.json
`
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	importCmd := cli.NewRootCmd()
	importCmd.SetArgs([]string{"--config", cfgPath, "trace", "import", harAbs})
	if err := importCmd.Execute(); err != nil {
		t.Fatalf("trace import: %v", err)
	}

	var buf strings.Builder
	inspectCmd := cli.NewRootCmd()
	inspectCmd.SetOut(&buf)
	inspectCmd.SetArgs([]string{"--config", cfgPath, "trace", "inspect", "--output", "json"})
	if err := inspectCmd.Execute(); err != nil {
		t.Fatalf("trace inspect: %v", err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(buf.String()), &doc); err != nil {
		t.Fatalf("inspect JSON: %v\n%s", err, buf.String())
	}
	for _, key := range []string{"schema_version", "operations", "sequences"} {
		if _, ok := doc[key]; !ok {
			t.Errorf("missing top-level key %q", key)
		}
	}
	var ops map[string]json.RawMessage
	if err := json.Unmarshal(doc["operations"], &ops); err != nil {
		t.Fatalf("operations: %v", err)
	}
	if _, ok := ops["CreateOrder"]; !ok {
		t.Error("expected operations.CreateOrder in inspect JSON")
	}
}

func TestTraceInspect_SubprocessJSON_jqFriendly(t *testing.T) {
	root := testutil.RepoRoot(t)
	bin := filepath.Join(root, "bt")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("bt binary missing; go build -o bt ./cmd/bt")
	}
	harAbs := filepath.Join(root, "examples/orders-api/bt/trace/sample.har")
	cfgAbs := filepath.Join(root, "examples/orders-api/bt/backendtest.yaml")

	importCmd := exec.Command(bin, "--config", cfgAbs, "trace", "import", harAbs)
	importCmd.Dir = root
	importCmd.Stderr = os.Stderr
	if err := importCmd.Run(); err != nil {
		t.Fatalf("trace import: %v", err)
	}

	out, err := exec.Command(bin, "--config", cfgAbs, "trace", "inspect", "--output", "json").Output()
	if err != nil {
		t.Fatalf("trace inspect: %v\n%s", err, out)
	}
	if !json.Valid(out) {
		t.Fatalf("inspect output is not valid JSON")
	}
}
