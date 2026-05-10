package contract

import (
	"fmt"
	"os"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/jayimbery/bt/pkg/model"
)

// Baseline captures known-failing contract operations (quarantine list).
type Baseline struct {
	Version     int               `yaml:"version"`
	Quarantined []BaselineEntry   `yaml:"quarantined"`
}

// BaselineEntry is one quarantined operation_id.
type BaselineEntry struct {
	OperationID     string `yaml:"operation_id"`
	Reason          string `yaml:"reason"`
	QuarantineUntil string `yaml:"quarantine_until,omitempty"`
}

// ContractResult is the outcome of one contract operation (used by baseline and exit-code helpers).
type ContractResult struct {
	OperationID string
	Passed      bool
	Violations  []ContractViolation
}

// AnnotatedResult combines a contract outcome with baseline metadata.
type AnnotatedResult struct {
	ContractResult
	Quarantined       bool
	QuarantineReason  string
	QuarantineExpired bool
	StaleBaseline     bool
}

// Failed reports whether this result should fail CI (quarantined active failures do not).
func (a AnnotatedResult) Failed() bool {
	if a.StaleBaseline {
		return false
	}
	if a.Quarantined && !a.QuarantineExpired {
		return false
	}
	return !a.Passed
}

// LoadBaseline reads and parses a baseline YAML file.
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("baseline: read %q: %w", path, err)
	}
	var b Baseline
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("baseline: parse %q: %w", path, err)
	}
	return &b, nil
}

// Annotate applies baseline rules to a contract result for one operation.
func (b *Baseline) Annotate(res ContractResult) AnnotatedResult {
	out := AnnotatedResult{ContractResult: res}
	if b == nil {
		return out
	}
	entry := b.findEntry(res.OperationID)
	if entry == nil {
		return out
	}
	if res.Passed {
		out.StaleBaseline = true
		out.QuarantineReason = entry.Reason
		return out
	}
	if entry.quarantineExpired() {
		out.QuarantineExpired = true
		return out
	}
	out.Quarantined = true
	out.QuarantineReason = entry.Reason
	return out
}

func (b *Baseline) findEntry(opID string) *BaselineEntry {
	for i := range b.Quarantined {
		if strings.EqualFold(strings.TrimSpace(b.Quarantined[i].OperationID), strings.TrimSpace(opID)) {
			return &b.Quarantined[i]
		}
	}
	return nil
}

func (e *BaselineEntry) quarantineExpired() bool {
	raw := strings.TrimSpace(e.QuarantineUntil)
	if raw == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return false
	}
	return time.Now().UTC().Truncate(24*time.Hour).After(t)
}

// AnnotateResults applies the baseline to model.Results from a contract run.
func AnnotateResults(results []model.Result, bl *Baseline) []AnnotatedResult {
	out := make([]AnnotatedResult, 0, len(results))
	for _, r := range results {
		v := violationsFromFailures(r.Failures)
		cr := ContractResult{
			OperationID: r.OperationID,
			Passed:      r.Passed,
			Violations:  v,
		}
		if bl == nil {
			out = append(out, AnnotatedResult{ContractResult: cr})
			continue
		}
		out = append(out, bl.Annotate(cr))
	}
	return out
}

// ApplyBaselineToResults mutates results in place with baseline flags (for reporting).
func ApplyBaselineToResults(results []model.Result, bl *Baseline) {
	ann := AnnotateResults(results, bl)
	for i := range results {
		if i >= len(ann) {
			break
		}
		a := ann[i]
		results[i].Quarantined = a.Quarantined
		results[i].QuarantineReason = a.QuarantineReason
		results[i].QuarantineExpired = a.QuarantineExpired
		results[i].StaleBaseline = a.StaleBaseline
	}
}

func violationsFromFailures(ff []model.Failure) []ContractViolation {
	var out []ContractViolation
	for _, f := range ff {
		if f.Invariant != model.InvariantContract {
			continue
		}
		sev := Critical
		if strings.EqualFold(f.Classification, "warning") {
			sev = Warning
		}
		exp := ""
		if f.Expected != nil {
			exp = fmt.Sprint(f.Expected)
		}
		act := ""
		if f.Actual != nil {
			act = fmt.Sprint(f.Actual)
		}
		out = append(out, ContractViolation{
			Field:    f.Path,
			Expected: exp,
			Actual:   act,
			Severity: sev,
		})
	}
	return out
}
