package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jimbery/bt/internal/config"
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
			outputFormat, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}

			_, loadErr := config.Load(path)

			if strings.EqualFold(strings.TrimSpace(outputFormat), "json") {
				out, mErr := marshalValidateJSON(loadErr)
				if mErr != nil {
					return mErr
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(out)); err != nil {
					return err
				}
				if loadErr != nil {
					return fmt.Errorf("config: %w", loadErr)
				}
				return nil
			}

			if loadErr != nil {
				return fmt.Errorf("config: %w", loadErr)
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "config is valid\n")
			return nil
		},
	}
}

func marshalValidateJSON(loadErr error) ([]byte, error) {
	if loadErr == nil {
		return json.Marshal(map[string]any{
			"valid":  true,
			"errors": []any{},
		})
	}
	if errors.Is(loadErr, config.ErrConfigNotFound) {
		return json.Marshal(map[string]any{
			"valid": false,
			"errors": []map[string]string{
				{"field": "config_path", "message": "config file not found"},
			},
		})
	}
	return json.Marshal(map[string]any{
		"valid": false,
		"errors": []map[string]string{
			{"field": "config_path", "message": loadErr.Error()},
		},
	})
}
