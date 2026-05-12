package report

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

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
			if !res.Passed {
				writeFailureResponseBody(r.w, res.Response.Body)
			}
			continue
		}

		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		nSchema := len(res.SchemaViolations)
		if nSchema > 0 {
			_, _ = fmt.Fprintf(r.w, "  %s  %s  (HTTP %d, schema: %d violation%s, %s)\n",
				status, res.CaseID, res.StatusCode, nSchema, pluralS(nSchema), res.Duration)
		} else {
			_, _ = fmt.Fprintf(r.w, "  %s  %s  (HTTP %d, %s)\n",
				status, res.CaseID, res.StatusCode, res.Duration)
		}
		for _, f := range res.Failures {
			label := f.Invariant
			if f.Classification != "" {
				label = f.Classification
			}
			_, _ = fmt.Fprintf(r.w, "       %s: %s\n", label, f.Message)
		}
		for _, sv := range res.SchemaViolations {
			_, _ = fmt.Fprintf(r.w, "       %s  %s [%s]\n", sv.Field, sv.Message, sv.Severity)
		}
		if res.ArtifactPath != "" {
			_, _ = fmt.Fprintf(r.w, "       artifact: %s\n", res.ArtifactPath)
			_, _ = fmt.Fprintf(r.w, "       replay:   bt replay %s\n", filepath.ToSlash(res.ArtifactPath))
		}
		if !res.Passed {
			writeFailureResponseBody(r.w, res.Response.Body)
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
	if s.SchemaViolations > 0 {
		line += fmt.Sprintf(" (%d schema violation%s)", s.SchemaViolations, pluralS(s.SchemaViolations))
	}
	if s.Quarantined > 0 {
		line += fmt.Sprintf(", %d quarantined", s.Quarantined)
	}
	if s.Skipped > 0 {
		line += fmt.Sprintf(", %d skipped", s.Skipped)
	}
	_, _ = fmt.Fprintf(r.w, "%s\n", line)
	return nil
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func writeFailureResponseBody(w io.Writer, body []byte) {
	if len(body) == 0 {
		return
	}
	txt := FailureBodyForDisplay(body)
	if txt == "" {
		return
	}
	_, _ = fmt.Fprintln(w, "       response body:")
	for _, line := range strings.Split(txt, "\n") {
		_, _ = fmt.Fprintf(w, "         %s\n", line)
	}
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
