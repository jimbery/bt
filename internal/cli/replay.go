package cli

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/internal/runner"
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
				return fmt.Errorf("cannot load config: %w", err)
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

			r := runner.New(runner.Config{
				BaseURL: cfg.Target.BaseURL,
				Timeout: 30 * time.Second,
			})

			input := artifact.Request.AsCaseInput()
			resp, err := r.Run(cmd.Context(), input)
			if err != nil {
				return fmt.Errorf("replay request failed: %w", err)
			}

			_, _ = fmt.Fprintf(w, "New response:      HTTP %d\n\n", resp.StatusCode)

			stillFailing := false
			for _, f := range artifact.Failures {
				if failureStillPresentAfterReplay(f, resp) {
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

var headerFailureNameRE = regexp.MustCompile(`header "([^"]+)"`)

func failureStillPresentAfterReplay(f model.Failure, resp model.ResponseDetail) bool {
	switch f.Invariant {
	case "status_code":
		want, ok := expectedAsInt(f.Expected)
		if !ok {
			return false
		}
		return resp.StatusCode != want
	case "response_header":
		name, ok := headerNameFromFailureMessage(f.Message)
		if !ok {
			return false
		}
		want, ok := expectedAsString(f.Expected)
		if !ok {
			return false
		}
		key := http.CanonicalHeaderKey(name)
		got := resp.Headers[key]
		return got != want
	default:
		return false
	}
}

func headerNameFromFailureMessage(msg string) (string, bool) {
	m := headerFailureNameRE.FindStringSubmatch(msg)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

func expectedAsInt(v any) (int, bool) {
	switch x := v.(type) {
	case float64:
		return int(x), true
	case int:
		return x, true
	case int64:
		return int(x), true
	case string:
		i, err := strconv.Atoi(strings.TrimSpace(x))
		return i, err == nil
	default:
		return 0, false
	}
}

func expectedAsString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case float64:
		return strconv.FormatInt(int64(x), 10), true
	case int:
		return strconv.Itoa(x), true
	case int64:
		return strconv.FormatInt(x, 10), true
	default:
		return "", false
	}
}
