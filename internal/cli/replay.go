package cli

import "github.com/spf13/cobra"

func newReplayCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "replay",
		Short:         "Replay a test artifact",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
}
