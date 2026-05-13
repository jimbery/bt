package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/internal/runner"
	gqlrunner "github.com/jayimbery/bt/internal/runner/graphql"
	"github.com/jayimbery/bt/internal/strategy/stateful"
	"github.com/jayimbery/bt/pkg/model"
)

func newReplayCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "replay <artifact-path>",
		Short:         "Replay a test artifact against the current target",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			artifactPath := args[0]
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}

			loader := replay.NewLoader()
			artifact, err := loader.Load(artifactPath)
			if errors.Is(err, replay.ErrArtifactNotFound) {
				return fmt.Errorf("%w: %s", replay.ErrArtifactNotFound, artifactPath)
			}
			if err != nil {
				return fmt.Errorf("cannot load artifact: %w", err)
			}

			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}

			w := cmd.OutOrStdout()

			_, _ = fmt.Fprintf(w, "Replaying artifact: %s\n", artifactPath)
			_, _ = fmt.Fprintf(w, "Case:               %s\n", artifact.CaseID)
			_, _ = fmt.Fprintf(w, "Strategy:           %s\n", artifact.StrategyKind)
			_, _ = fmt.Fprintf(w, "Recorded at:        %s\n", artifact.OccurredAt.Format(time.RFC3339))
			_, _ = fmt.Fprintf(w, "\nOriginal failure:\n")
			for _, f := range artifact.Failures {
				_, _ = fmt.Fprintf(w, "  [%s] %s\n", f.Invariant, f.Message)
			}

			_, _ = fmt.Fprintf(w, "\nOriginal response: HTTP %d\n", artifact.Response.StatusCode)
			_, _ = fmt.Fprintf(w, "\nRe-executing request against %s...\n", cfg.Target.BaseURL)

			baseURL := strings.TrimRight(strings.TrimSpace(cfg.Target.BaseURL), "/")

			if strings.EqualFold(strings.TrimSpace(artifact.StrategyKind), "stateful") {
				if artifact.StatefulFlow == nil || artifact.StatefulResult == nil {
					return fmt.Errorf("stateful artifact missing stateful_flow or stateful_result")
				}
				rnr := stateful.NewRunner(stateful.Config{BaseURL: baseURL})
				fr, repErr := rnr.ReplayArtifact(cmd.Context(), artifact)
				if repErr != nil {
					return fmt.Errorf("stateful replay: %w", repErr)
				}
				_, _ = fmt.Fprintf(w, "Replayed flow %q: %d steps, passed=%v\n", fr.FlowID, len(fr.Steps), fr.Passed)
				if !fr.Passed {
					_, _ = fmt.Fprintf(w, "Result: FAIL — flow still failing after replay\n")
					return fmt.Errorf("%w", ErrReplayFailurePresent)
				}
				_, _ = fmt.Fprintf(w, "Result: PASS — flow passes on replay\n")
				return nil
			}

			input := artifact.Request.AsCaseInput()
			var resp model.ResponseDetail
			var runErr error
			if input.IsGraphQL() {
				gqlExec := gqlrunner.New(gqlrunner.Config{
					BaseURL: baseURL,
					Timeout: runner.DefaultTimeout,
				})
				resp, runErr = gqlExec.Run(cmd.Context(), input)
			} else {
				r := runner.New(runner.Config{
					BaseURL: baseURL,
					Timeout: runner.DefaultTimeout,
				})
				resp, runErr = r.Run(cmd.Context(), input)
			}
			if runErr != nil {
				return fmt.Errorf("replay request failed: %w", runErr)
			}

			_, _ = fmt.Fprintf(w, "New response:      HTTP %d\n\n", resp.StatusCode)

			stillFailing := false
			for _, f := range artifact.Failures {
				if replay.FailureStillPresentAfterReplay(f, resp, artifact.Expected) {
					stillFailing = true
					break
				}
			}

			if stillFailing {
				_, _ = fmt.Fprintf(w, "Result: FAIL — failure still present\n")
				return fmt.Errorf("%w", ErrReplayFailurePresent)
			}

			_, _ = fmt.Fprintf(w, "Result: PASS — failure no longer reproducible\n")
			return nil
		},
	}
}
