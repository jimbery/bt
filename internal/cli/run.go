package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/jayimbery/bt/internal/adapter/openapi"
	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/internal/report"
	"github.com/jayimbery/bt/internal/runner"
	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/internal/strategy/property"
	"github.com/jayimbery/bt/internal/strategy/table"
	"github.com/jayimbery/bt/pkg/model"
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
				return fmt.Errorf("config: %w", err)
			}

			ad := openapi.New()
			target := cfg.Target.AsModel()
			if err := ad.Validate(cmd.Context(), target); err != nil {
				return fmt.Errorf("adapter validate: %w", err)
			}
			ops, err := ad.Discover(cmd.Context(), target)
			if err != nil {
				return fmt.Errorf("adapter: %w", err)
			}

			st, spec, err := buildStrategyAndSpec(cfgPath, strategyName, cfg)
			if err != nil {
				return err
			}
			if strategy.Kind(strategyName) == strategy.KindProperty {
				if cmd.Flags().Changed("seed") {
					seed, err := cmd.Flags().GetInt64("seed")
					if err != nil {
						return err
					}
					if spec.Config == nil {
						spec.Config = map[string]any{}
					}
					spec.Config["seed"] = seed
				}
				if cmd.Flags().Changed("checks") {
					checks, err := cmd.Flags().GetInt("checks")
					if err != nil {
						return err
					}
					if spec.Config == nil {
						spec.Config = map[string]any{}
					}
					spec.Config["checks"] = checks
				}
			}

			cases, err := st.Plan(cmd.Context(), spec, ops)
			if err != nil {
				return fmt.Errorf("plan: %w", err)
			}

			exec := runner.New(runner.Config{
				BaseURL: cfg.Target.BaseURL,
				Timeout: runner.DefaultTimeout,
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

	cmd.Flags().Int64("seed", 0, "PRNG seed for property strategy (rapid); omit for random seed")
	cmd.Flags().Int("checks", 0, "number of property checks per operation (rapid); 0 uses default from strategy")
	return cmd
}

func buildStrategyAndSpec(cfgPath, strategyName string, cfg *config.Config) (strategy.Strategy, strategy.Spec, error) {
	var st strategy.Strategy
	switch strategy.Kind(strategyName) {
	case strategy.KindTable:
		artifactDir := filepath.Join(filepath.Dir(cfgPath), ".bt", "artifacts")
		st = table.NewWithOptions(table.Options{
			ArtifactWriter: replay.NewWriter(artifactDir),
			Environment:    cfg.Target.Environment,
		})
	case strategy.KindProperty:
		artifactDir := filepath.Join(filepath.Dir(cfgPath), ".bt", "artifacts")
		st = property.NewWithOptions(property.Options{
			ArtifactWriter: replay.NewWriter(artifactDir),
			Environment:    cfg.Target.Environment,
		})
	default:
		return nil, strategy.Spec{}, fmt.Errorf("unknown strategy: %q", strategyName)
	}

	spec := strategy.Spec{Kind: strategy.Kind(strategyName)}
	var found bool
	for _, sc := range cfg.Strategies {
		if sc.Type != strategyName {
			continue
		}
		found = true
		spec.Operations = append([]string(nil), sc.Operations...)
		for _, name := range sc.Invariants {
			spec.Invariants = append(spec.Invariants, model.Invariant{Name: name})
		}
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
		return nil, strategy.Spec{}, fmt.Errorf("no strategy of type %q found in config", strategyName)
	}
	return st, spec, nil
}
