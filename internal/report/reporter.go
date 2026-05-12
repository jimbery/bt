package report

import (
	"github.com/jayimbery/bt/pkg/model"
)

// Reporter writes test results to an io.Writer in a specific format.
type Reporter interface {
	Write(results []model.Result) error
}

type summary struct {
	Total            int
	Passed           int
	Failed           int
	Skipped          int
	Quarantined      int
	SchemaViolations int
}

func summarise(results []model.Result) summary {
	s := summary{Total: len(results)}
	for _, r := range results {
		s.SchemaViolations += len(r.SchemaViolations)
		if r.Skipped {
			s.Skipped++
			continue
		}
		if r.Quarantined && !r.Passed {
			s.Quarantined++
			continue
		}
		if r.Passed {
			s.Passed++
		} else {
			s.Failed++
		}
	}
	return s
}
