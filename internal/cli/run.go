package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/jayimbery/bt/internal/adapter/openapi"
	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/report"
	"github.com/jayimbery/bt/internal/runner"
	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/internal/strategy/table"
)

func newRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "run",
		Short:         "Run a test plan",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			strategyName, err := cmd.Flags().GetString("strategy")
			if err != nil {
				return err
			}
			if strategyName == "" {
				strategyName = "table"
			}
			outputFormat, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}

			ad := openapi.New()
			ops, err := ad.Discover(cmd.Context(), cfg.Target.AsModel())
			if err != nil {
				return fmt.Errorf("adapter: %w", err)
			}

			var st strategy.Strategy
			switch strategy.Kind(strategyName) {
			case strategy.KindTable:
				st = table.New()
			default:
				return fmt.Errorf("unknown strategy: %q", strategyName)
			}

			spec := strategy.Spec{Kind: strategy.Kind(strategyName)}
			var found bool
			for _, sc := range cfg.Strategies {
				if sc.Type != strategyName {
					continue
				}
				found = true
				if sc.Config != nil {
					spec.Config = make(map[string]any, len(sc.Config)+1)
					for k, v := range sc.Config {
						spec.Config[k] = v
					}
				} else {
					spec.Config = map[string]any{}
				}
				if sc.File != "" {
					spec.Config["file"] = sc.File
				}
				break
			}
			if !found {
				return fmt.Errorf("no strategy of type %q found in config", strategyName)
			}

			cases, err := st.Plan(cmd.Context(), spec, ops)
			if err != nil {
				return fmt.Errorf("plan: %w", err)
			}

			exec := runner.New(runner.Config{
				BaseURL: cfg.Target.BaseURL,
				Timeout: 30 * time.Second,
			})

			results, err := st.Execute(cmd.Context(), cases, exec)
			if err != nil {
				return fmt.Errorf("execute: %w", err)
			}

			var rep report.Reporter
			switch outputFormat {
			case "json":
				rep = report.NewJSON(cmd.OutOrStdout())
			case "junit":
				rep = report.NewJUnit(cmd.OutOrStdout())
			default:
				rep = report.NewConsole(cmd.OutOrStdout())
			}

			if err := rep.Write(results); err != nil {
				return fmt.Errorf("report: %w", err)
			}

			for _, res := range results {
				if !res.Passed {
					return ErrTestFailures
				}
			}

			return nil
		},
	}

	return cmd
}
