package cli

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/doctor"
	"github.com/jayimbery/bt/internal/exitcode"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "doctor",
		Short:         "Check environment and config before a run",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			cfgDir := filepath.Dir(cfgPath)
			dc := doctor.Config{
				SchemaPath:    "",
				TargetBaseURL: "",
				AuthEnvVar:    "",
				BaselinePath:  filepath.Join(cfgDir, ".bt", "baseline.yaml"),
			}
			if cfg, loadErr := config.Load(cfgPath); loadErr == nil {
				dc.SchemaPath = cfg.Target.SchemaPath
				dc.TargetBaseURL = cfg.Target.BaseURL
				dc.AuthEnvVar = cfg.Target.Auth.Env
				if strings.TrimSpace(cfg.Baseline) != "" {
					if filepath.IsAbs(cfg.Baseline) {
						dc.BaselinePath = cfg.Baseline
					} else {
						dc.BaselinePath = filepath.Join(cfgDir, cfg.Baseline)
					}
				}
			} else {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not load config (%v); schema and target checks may be incomplete\n", loadErr)
			}

			results := doctor.RunAll(dc)
			outFmt, _ := cmd.Root().PersistentFlags().GetString("output")
			if outFmt == "" {
				outFmt = "console"
			}
			switch outFmt {
			case "json":
				b, err := doctor.FormatJSON(results)
				if err != nil {
					return err
				}
				_, _ = fmt.Fprintln(cmd.OutOrStdout(), string(b))
			default:
				title := "bt doctor — environment check"
				_, _ = fmt.Fprint(cmd.OutOrStdout(), doctor.FormatConsole(title, results))
			}

			for _, r := range results {
				if !r.Passed && r.Level == doctor.Fail {
					return exitcode.WrapConfig(errors.New("doctor: one or more blocking checks failed"))
				}
			}
			return nil
		},
	}
}
