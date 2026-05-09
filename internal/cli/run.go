package cli

import "github.com/spf13/cobra"

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "run",
		Short:         "Run a test plan",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}
