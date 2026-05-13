package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/exitcode"
	"github.com/jayimbery/bt/internal/report"
	"github.com/jayimbery/bt/internal/runplan"
	"github.com/jayimbery/bt/internal/strategy/contract"
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
			runAll := strings.EqualFold(strings.TrimSpace(strategyName), "all")
			strategyList := []string{strategyName}
			if runAll {
				strategyList = []string{"table", "property", "fuzz", "contract", "stateful"}
			}
			outputFormat, err := cmd.Flags().GetString("output")
			if err != nil {
				return err
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return exitcode.WrapConfig(fmt.Errorf("config: %w", err))
			}

			loadDotenv, err := cmd.Flags().GetBool("load-dotenv")
			if err != nil {
				return err
			}
			if loadDotenv {
				cfgDir := filepath.Dir(cfgPath)
				for _, p := range []string{
					filepath.Join(cfgDir, ".env"),
					filepath.Join(cfgDir, "..", ".env"),
					".env",
				} {
					if err := config.LoadDotEnvFile(p); err != nil {
						return exitcode.WrapConfig(fmt.Errorf("load-dotenv %q: %w", p, err))
					}
				}
			}

			if strings.EqualFold(strings.TrimSpace(cfg.Target.Auth.Type), "bearer") {
				env := strings.TrimSpace(cfg.Target.Auth.Env)
				if env != "" && strings.TrimSpace(os.Getenv(env)) == "" {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(),
						"warning: target.auth bearer uses env %q but it is not set or empty in this process — Authorization will not be sent (hint: pass --load-dotenv to read .env next to the config file)\n", env)
				}
			}

			adapterName := strings.TrimSpace(cfg.Target.Adapter)
			if cmd.Root().PersistentFlags().Changed("adapter") {
				v, err := cmd.Root().PersistentFlags().GetString("adapter")
				if err != nil {
					return err
				}
				adapterName = strings.TrimSpace(v)
			}

			target := cfg.Target.AsModel()
			ad := runplan.AdapterForName(adapterName)
			if err := ad.Validate(cmd.Context(), target); err != nil {
				return exitcode.WrapConfig(fmt.Errorf("adapter validate: %w", err))
			}
			ops, err := ad.Discover(cmd.Context(), target)
			if err != nil {
				return exitcode.WrapConfig(fmt.Errorf("adapter: %w", err))
			}

			opt := runplan.BuildOptions{Stderr: cmd.ErrOrStderr()}
			if cmd.Flags().Changed("safety") {
				v, err := cmd.Flags().GetString("safety")
				if err != nil {
					return err
				}
				opt.SafetyFlagChanged = true
				opt.SafetyFlagValue = v
			}
			if cmd.Flags().Changed("seed") {
				seed, err := cmd.Flags().GetInt64("seed")
				if err != nil {
					return err
				}
				opt.SeedProvided = true
				opt.Seed = seed
			}
			if cmd.Flags().Changed("checks") {
				checks, err := cmd.Flags().GetInt("checks")
				if err != nil {
					return err
				}
				opt.ChecksProvided = true
				opt.Checks = checks
			}
			if cmd.Flags().Changed("fuzz-iterations") {
				n, err := cmd.Flags().GetInt("fuzz-iterations")
				if err != nil {
					return err
				}
				opt.FuzzIterationsProvided = true
				opt.FuzzIterations = n
			}
			if cmd.Flags().Changed("corpus-dir") {
				dir, err := cmd.Flags().GetString("corpus-dir")
				if err != nil {
					return err
				}
				opt.CorpusDirProvided = true
				opt.CorpusDir = dir
			}

			exec := runplan.BuildDefaultExecutor(cfg, adapterName)

			var results []model.Result
			for _, stratName := range strategyList {
				if runAll {
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "\n== strategy: %s ==\n", stratName)
				}
				st, spec, err := runplan.BuildStrategyAndSpec(cfgPath, stratName, cfg, opt)
				if err != nil {
					if runAll && strings.Contains(err.Error(), "no strategy of type") {
						_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "skip %s: %v\n", stratName, err)
						continue
					}
					return exitcode.WrapConfig(err)
				}

				cases, err := st.Plan(cmd.Context(), spec, ops)
				if err != nil {
					return exitcode.WrapConfig(fmt.Errorf("plan (%s): %w", stratName, err))
				}
				excludeCSV, err := cmd.Flags().GetString("exclude")
				if err != nil {
					return err
				}
				cases = runplan.FilterExcludedCases(cases, excludeCSV)
				runplan.AttachResolvedOperations(cases, ops)

				part, err := st.Execute(cmd.Context(), cases, exec)
				if err != nil {
					return exitcode.WrapExecution(fmt.Errorf("execute (%s): %w", stratName, err))
				}
				results = append(results, part...)
			}

			bp := filepath.Join(filepath.Dir(cfgPath), ".bt", "baseline.yaml")
			if strings.TrimSpace(cfg.Baseline) != "" {
				if filepath.IsAbs(cfg.Baseline) {
					bp = cfg.Baseline
				} else {
					bp = filepath.Join(filepath.Dir(cfgPath), cfg.Baseline)
				}
			}
			if _, statErr := os.Stat(bp); statErr == nil {
				if bl, berr := contract.LoadBaseline(bp); berr == nil {
					contract.ApplyBaselineToResults(results, bl)
					for _, r := range results {
						if r.StaleBaseline {
							_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: stale baseline entry for operation %q (still listed as quarantined but now passes)\n", r.OperationID)
						}
					}
				}
			}

			outPath, err := cmd.Flags().GetString("output-file")
			if err != nil {
				return err
			}
			reportOut := cmd.OutOrStdout()
			if outPath != "" {
				if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
					return exitcode.WrapConfig(fmt.Errorf("output-file: %w", err))
				}
				f, err := os.Create(outPath)
				if err != nil {
					return exitcode.WrapConfig(fmt.Errorf("output-file: %w", err))
				}
				defer func() { _ = f.Close() }()
				switch outputFormat {
				case "json", "junit":
					reportOut = f
				default:
					reportOut = io.MultiWriter(cmd.OutOrStdout(), f)
				}
			}

			var rep report.Reporter
			switch outputFormat {
			case "json":
				rep = report.NewJSON(reportOut)
			case "junit":
				rep = report.NewJUnit(reportOut)
			default:
				rep = report.NewConsole(reportOut)
			}

			if err := rep.Write(results); err != nil {
				return exitcode.WrapConfig(fmt.Errorf("report: %w", err))
			}

			for _, res := range results {
				if !res.Passed && !res.Skipped && !res.Quarantined {
					return ErrTestFailures
				}
			}

			return nil
		},
	}

	cmd.Flags().Int64("seed", 0, "PRNG seed for property strategy (rapid); omit for random seed")
	cmd.Flags().Int("checks", 0, "number of property checks per operation (rapid); 0 uses default from strategy")
	cmd.Flags().String("output-file", "", "write report to this path (json/junit only; console also copies when set)")
	cmd.Flags().String("safety", "", "safety profile override for fuzz strategy: safe, aggressive, destructive")
	cmd.Flags().Int("fuzz-iterations", 0, "max HTTP attempts per operation for fuzz strategy (0 = use config or default 50)")
	cmd.Flags().String("corpus-dir", "", "corpus directory for fuzz seeds (default: <config-dir>/corpus)")
	cmd.Flags().Bool("load-dotenv", false, "before resolving target.auth, load unset keys from .env files (<config-dir>/.env, parent/.env, ./.env); never overrides existing environment variables")
	cmd.Flags().String("exclude", "", "comma-separated table case IDs to skip (not included in the report)")
	return cmd
}
