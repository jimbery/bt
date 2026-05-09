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
		status := "PASS"
		if !res.Passed {
			status = "FAIL"
		}
		_, _ = fmt.Fprintf(r.w, "  %s  %s  (HTTP %d, %s)\n",
			status, res.CaseID, res.StatusCode, res.Duration)
		for _, f := range res.Failures {
			_, _ = fmt.Fprintf(r.w, "       %s: %s\n", f.Invariant, f.Message)
		}
		if res.ArtifactPath != "" {
			_, _ = fmt.Fprintf(r.w, "       artifact: %s\n", res.ArtifactPath)
			_, _ = fmt.Fprintf(r.w, "       replay:   bt replay %s\n", filepath.ToSlash(res.ArtifactPath))
		}
	}

	s := summarise(results)
	_, _ = fmt.Fprintf(r.w, "\n%d tests run: %d passed, %d failed\n", s.Total, s.Passed, s.Failed)
	return nil
}
