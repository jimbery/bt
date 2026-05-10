package report

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/jayimbery/bt/pkg/model"
)

type consoleReporter struct{ w io.Writer }

// NewConsole returns a Reporter that writes a human-readable summary.
func NewConsole(w io.Writer) Reporter { return &consoleReporter{w: w} }

func (r *consoleReporter) Write(results []model.Result) error {
	for _, res := range results {
		if res.Skipped {
			_, _ = fmt.Fprintf(r.w, "  SKIP  %s", res.CaseID)
			if res.StrategyKind != "" {
				_, _ = fmt.Fprintf(r.w, " [%s]", res.StrategyKind)
			}
			if res.SkipReason != "" {
				_, _ = fmt.Fprintf(r.w, "        %s", res.SkipReason)
			}
			_, _ = fmt.Fprintf(r.w, "\n")
			continue
		}

		if res.StrategyKind == "fuzz" {
			status := "PASS"
			if !res.Passed {
				status = "FAIL"
			}
			nFail := len(res.Failures)
			_, _ = fmt.Fprintf(r.w, "  %s  %s", status, res.CaseID)
			if res.StrategyKind != "" {
				_, _ = fmt.Fprintf(r.w, " [%s]", res.StrategyKind)
			}
			_, _ = fmt.Fprintf(r.w, "           %d mutations", res.MutationCount)
			if nFail > 0 {
				_, _ = fmt.Fprintf(r.w, "  (%d failures)", nFail)
			}
			_, _ = fmt.Fprintf(r.w, "  (HTTP %d, %s)\n", res.StatusCode, res.Duration)
			for _, f := range res.Failures {
				label := f.Invariant
				if f.Classification != "" {
					label = f.Classification
				}
				_, _ = fmt.Fprintf(r.w, "       %s: %s\n", label, f.Message)
				if f.MutatedInput != "" {
					_, _ = fmt.Fprintf(r.w, "         input:           %s\n", f.MutatedInput)
				}
				if f.ArtifactPath != "" {
					_, _ = fmt.Fprintf(r.w, "         artifact:        %s\n", f.ArtifactPath)
					_, _ = fmt.Fprintf(r.w, "         replay:   bt replay %s\n", filepath.ToSlash(f.ArtifactPath))
				}
			}
			if res.ArtifactPath != "" && nFail == 0 {
				_, _ = fmt.Fprintf(r.w, "       artifact: %s\n", res.ArtifactPath)
				_, _ = fmt.Fprintf(r.w, "       replay:   bt replay %s\n", filepath.ToSlash(res.ArtifactPath))
			}
			continue
		}

		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		_, _ = fmt.Fprintf(r.w, "  %s  %s  (HTTP %d, %s)\n",
			status, res.CaseID, res.StatusCode, res.Duration)
		for _, f := range res.Failures {
			label := f.Invariant
			if f.Classification != "" {
				label = f.Classification
			}
			_, _ = fmt.Fprintf(r.w, "       %s: %s\n", label, f.Message)
		}
		if res.ArtifactPath != "" {
			_, _ = fmt.Fprintf(r.w, "       artifact: %s\n", res.ArtifactPath)
			_, _ = fmt.Fprintf(r.w, "       replay:   bt replay %s\n", filepath.ToSlash(res.ArtifactPath))
		}
	}

	s := summarise(results)
	label := "tests"
	for _, res := range results {
		if res.StrategyKind == "fuzz" {
			label = "operations"
			break
		}
	}
	line := fmt.Sprintf("\n%d %s run: %d passed, %d failed", s.Total, label, s.Passed, s.Failed)
	if s.Skipped > 0 {
		line += fmt.Sprintf(", %d skipped", s.Skipped)
	}
	_, _ = fmt.Fprintf(r.w, "%s\n", line)
	return nil
}

// ConsoleReporter is the concrete M5 console reporter; use [NewConsoleReporter] when calling [ConsoleReporter.Render].
type ConsoleReporter struct {
	rep Reporter
}

// NewConsoleReporter returns a console reporter compatible with the M5 doc API.
func NewConsoleReporter(w io.Writer) *ConsoleReporter {
	return &ConsoleReporter{rep: NewConsole(w)}
}

// Render writes the same format as [Reporter.Write].
func (r *ConsoleReporter) Render(results []model.Result) error {
	return r.rep.Write(results)
}

// Write forwards to the underlying reporter.
func (r *ConsoleReporter) Write(results []model.Result) error {
	return r.rep.Write(results)
}
