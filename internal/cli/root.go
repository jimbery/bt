package cli

import "github.com/spf13/cobra"

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "bt",
		Short:         "Backend testing platform",
		Long:          "bt is a Go-native backend testing platform for table, property, fuzz, and contract testing strategies. Use `bt mcp serve` to expose tools over the Model Context Protocol.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().String("config", "backendtest.yaml", "config file path")
	root.PersistentFlags().String("env", "", "environment profile (local, ci, staging, preprod)")
	root.PersistentFlags().String("output", "console", "output format (console, json, junit)")
	root.PersistentFlags().String("strategy", "table", "strategy to run (table, property, fuzz, contract, or all)")
	root.PersistentFlags().String("adapter", "", "protocol adapter override (openapi, graphql); default from config target.adapter")

	root.AddCommand(newInitCmd())
	root.AddCommand(newValidateCmd())
	root.AddCommand(newRunCmd())
	root.AddCommand(newReplayCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newMcpCmd())

	return root
}

// Execute runs the CLI (for use from main).
func Execute() error {
	return NewRootCmd().Execute()
}
