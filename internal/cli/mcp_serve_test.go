package cli_test

import (
	"bytes"
	"testing"

	"github.com/jimbery/bt/internal/cli"
)

func TestMCPServeCommand_ExistsAsSubcommand(t *testing.T) {
	cmd := cli.NewRootCmd()
	mcpCmd, _, err := cmd.Find([]string{"mcp", "serve"})
	if err != nil || mcpCmd == nil {
		t.Error("expected 'bt mcp serve' to exist as a subcommand")
	}
}

func TestMCPServeCommand_ConfigFlagExists(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "serve", "--help"})
	_ = cmd.Execute()
	if !bytes.Contains(buf.Bytes(), []byte("--config")) {
		t.Error("expected --config flag in 'bt mcp serve --help'")
	}
}

func TestMCPServeCommand_AppearsInRootHelp(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})
	_ = cmd.Execute()
	if !bytes.Contains(buf.Bytes(), []byte("mcp")) {
		t.Error("expected 'mcp' to appear in root command help")
	}
}
