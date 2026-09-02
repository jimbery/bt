package cli

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	btmcpserver "github.com/jimbery/bt/internal/mcp"
)

func newMcpCmd() *cobra.Command {
	mcp := &cobra.Command{
		Use:   "mcp",
		Short: "Model Context Protocol server for AI clients",
	}
	serve := &cobra.Command{
		Use:           "serve",
		Short:         "Run the bt MCP server on stdio (stdout is the protocol stream; logs go to stderr)",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			ctx := context.Background()
			if err := btmcpserver.ServeStdio(ctx, cfgPath, cmd.ErrOrStderr()); err != nil {
				return fmt.Errorf("mcp serve: %w", err)
			}
			return nil
		},
	}
	serve.Flags().String("config", "backendtest.yaml", "default config path when tools omit config_path")
	mcp.AddCommand(serve)
	mcp.AddCommand(newMcpCallCmd())
	return mcp
}
