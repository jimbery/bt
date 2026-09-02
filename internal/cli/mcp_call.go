package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jimbery/bt/internal/mcp/testclient"
)

func newMcpCallCmd() *cobra.Command {
	var inputJSON, outputFmt, defaultCfg string

	cmd := &cobra.Command{
		Use:           "call <tool-name>",
		Short:         "Invoke one MCP tool via a short-lived stdio server (for CI and scripting)",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.ToLower(strings.TrimSpace(outputFmt)) != "json" {
				return fmt.Errorf("only --output json is supported")
			}
			var input any
			if strings.TrimSpace(inputJSON) == "" {
				input = map[string]any{}
			} else {
				if err := json.Unmarshal([]byte(inputJSON), &input); err != nil {
					return fmt.Errorf("parse --input: %w", err)
				}
			}

			btPath, err := os.Executable()
			if err != nil {
				return fmt.Errorf("executable path: %w", err)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()

			client, err := testclient.Start(ctx, btPath, defaultCfg)
			if err != nil {
				return fmt.Errorf("mcp client: %w", err)
			}
			defer func() { _ = client.Close() }()

			raw, err := client.Call(ctx, args[0], input)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if _, err := w.Write(raw); err != nil {
				return err
			}
			if len(raw) == 0 || raw[len(raw)-1] != '\n' {
				_, _ = w.Write([]byte("\n"))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&inputJSON, "input", "{}", "JSON object passed as tool arguments")
	cmd.Flags().StringVar(&defaultCfg, "config", "backendtest.yaml", "default config for mcp serve subprocess (bt_run / bt_validate when config_path omitted)")
	cmd.Flags().StringVar(&outputFmt, "output", "json", "output format (json only)")
	return cmd
}
