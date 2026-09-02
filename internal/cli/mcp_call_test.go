package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jimbery/bt/internal/cli"
	"github.com/jimbery/bt/internal/testutil"
)

func btBinaryForMCPCall(t *testing.T) string {
	t.Helper()
	p := filepath.Join(testutil.RepoRoot(t), "bt")
	if _, err := os.Stat(p); err != nil {
		t.Skip("bt binary not found; build with: go build -o bt ./cmd/bt")
	}
	return p
}

func TestMCPCallCommand_ExistsAsSubcommand(t *testing.T) {
	cmd := cli.NewRootCmd()
	callCmd, _, err := cmd.Find([]string{"mcp", "call"})
	if err != nil || callCmd == nil {
		t.Error("expected 'bt mcp call' to exist as a subcommand")
	}
}

func TestMCPCallCommand_InputFlagExists(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "call", "--help"})
	_ = cmd.Execute()
	if !bytes.Contains(buf.Bytes(), []byte("--input")) {
		t.Error("expected --input flag in 'bt mcp call --help'")
	}
}

func TestMCPCallCommand_UnknownTool_ExitsNonZero(t *testing.T) {
	bt := btBinaryForMCPCall(t)
	cmd := exec.Command(bt, "mcp", "call", "bt_does_not_exist", "--input", "{}")
	cmd.Dir = testutil.RepoRoot(t)
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Errorf("expected non-zero exit for unknown tool; output: %s", out)
	}
}

func TestMCPCallCommand_ValidTool_OutputIsJSON(t *testing.T) {
	bt := btBinaryForMCPCall(t)
	cmd := exec.Command(bt, "mcp", "call", "bt_validate",
		"--input", `{"config_path":"/does/not/exist.yaml"}`,
	)
	cmd.Dir = testutil.RepoRoot(t)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("mcp call: %v\n%s", err, cmd.Stderr)
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("expected valid JSON output: %v\nraw: %s", err, out)
	}
}
