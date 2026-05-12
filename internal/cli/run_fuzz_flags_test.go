package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jayimbery/bt/internal/cli"
)

func TestRunCommand_FuzzFlags_SafetyParsed(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--strategy", "fuzz", "--safety", "safe", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommand_FuzzFlags_IterationsParsed(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--strategy", "fuzz", "--fuzz-iterations", "200", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommand_FuzzFlags_CorpusDirParsed(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--strategy", "fuzz", "--corpus-dir", "/tmp/corpus", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunCommand_FuzzFlags_AllAppearInHelp(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	output := buf.String()
	for _, flag := range []string{"--safety", "--fuzz-iterations", "--corpus-dir", "--exclude"} {
		if !bytes.Contains([]byte(output), []byte(flag)) {
			t.Errorf("expected flag %q in 'bt run --help' output", flag)
		}
	}
}

func TestRunCommand_FuzzFlags_InvalidSafetyProfile_ReturnsError(t *testing.T) {
	t.Parallel()
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
	cfg := `version: 1
target:
  name: test-api
  base_url: http://127.0.0.1:9
  schema: ` + specPath + `
strategies:
  - type: fuzz
    operations:
      - GetHealth
    config:
      fuzz_iterations: 1
`
	cfgPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"run", "--config", cfgPath, "--strategy", "fuzz", "--safety", "yolo"})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error for invalid --safety profile value")
	}
}
