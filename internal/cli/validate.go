package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/jayimbery/bt/internal/config"
)

func newValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "validate",
		Short:         "Validate the backendtest.yaml config file",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			if _, err := config.Load(path); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "config is valid\n")
			return nil
		},
	}
}
