package report

import (
	"encoding/json"
	"io"

	"github.com/jayimbery/bt/pkg/model"
)

type jsonReporter struct{ w io.Writer }

// NewJSON returns a Reporter that writes a JSON report.
func NewJSON(w io.Writer) Reporter { return &jsonReporter{w: w} }

func (r *jsonReporter) Write(results []model.Result) error {
	s := summarise(results)
	out := map[string]any{
		"results": results,
		"summary": map[string]any{
			"total":  s.Total,
			"passed": s.Passed,
			"failed": s.Failed,
		},
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
