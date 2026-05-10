package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/jayimbery/bt/pkg/model"
)

type jsonReporter struct{ w io.Writer }

// NewJSON returns a Reporter that writes a JSON report.
func NewJSON(w io.Writer) Reporter { return &jsonReporter{w: w} }

func (r *jsonReporter) Write(results []model.Result) error {
	s := summarise(results)
	rows := make([]map[string]any, 0, len(results))
	for _, res := range results {
		rows = append(rows, resultJSONObject(res))
	}
	out := map[string]any{
		"results": rows,
		"summary": map[string]any{
			"total":       s.Total,
			"passed":      s.Passed,
			"failed":      s.Failed,
			"skipped":     s.Skipped,
			"quarantined": s.Quarantined,
		},
	}
	enc := json.NewEncoder(r.w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

func resultJSONObject(r model.Result) map[string]any {
	b, err := json.Marshal(&r)
	if err != nil {
		return map[string]any{"case_id": r.CaseID}
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return map[string]any{"case_id": r.CaseID}
	}
	sp := r.ContractSchemaRef
	if sp == "" && r.StrategyKind == "contract" {
		sp = "openapi-response"
	}
	if sp != "" {
		m["schema_path"] = sp
	}
	m["violations"] = contractViolationsJSON(r.Failures)
	return m
}

func contractViolationsJSON(ff []model.Failure) []map[string]any {
	out := make([]map[string]any, 0, len(ff))
	for _, f := range ff {
		if f.Invariant != model.InvariantContract {
			continue
		}
		sev := "critical"
		if f.Classification == "warning" {
			sev = "warning"
		}
		out = append(out, map[string]any{
			"field":    f.Path,
			"expected": violationString(f.Expected),
			"actual":   violationString(f.Actual),
			"severity": sev,
		})
	}
	return out
}

func violationString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		return fmt.Sprint(x)
	}
}
