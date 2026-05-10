package model

import (
	"encoding/json"
	"time"
)

type Result struct {
	CaseID        string         `json:"case_id"`
	Passed        bool           `json:"passed"`
	Skipped       bool           `json:"skipped,omitempty"`
	SkipReason    string         `json:"skip_reason,omitempty"`
	StrategyKind  string         `json:"strategy_kind,omitempty"`
	MutationCount int            `json:"mutation_count,omitempty"`
	StatusCode    int            `json:"status_code"`
	Duration      time.Duration  `json:"-"`
	Failures      []Failure      `json:"failures,omitempty"`
	Request       RequestDetail  `json:"request"`
	Response      ResponseDetail `json:"response"`
	ArtifactPath  string         `json:"artifact_path,omitempty"`
}

type Failure struct {
	Invariant      string `json:"invariant"`
	Classification string `json:"classification,omitempty"`
	Message        string `json:"message"`
	Path           string `json:"path,omitempty"`
	Expected       any    `json:"expected,omitempty"`
	Actual         any    `json:"actual,omitempty"`
	MutatedInput   string `json:"mutated_input,omitempty"`
	ArtifactPath   string `json:"artifact_path,omitempty"`
}

type RequestDetail struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

// AsCaseInput converts a RequestDetail back into a CaseInput for replay.
// The URL field holds the request path (same as CaseInput.Path).
func (r RequestDetail) AsCaseInput() CaseInput {
	ci := CaseInput{
		Method:  r.Method,
		Path:    r.URL,
		Headers: r.Headers,
		Query:   r.Query,
	}
	if len(r.Body) > 0 {
		var v any
		if err := json.Unmarshal(r.Body, &v); err == nil {
			ci.Body = v
		}
	}
	return ci
}

type ResponseDetail struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       []byte            `json:"body,omitempty"`
}

func (r Result) MarshalJSON() ([]byte, error) {
	type out struct {
		CaseID        string         `json:"case_id"`
		Passed        bool           `json:"passed"`
		Skipped       bool           `json:"skipped,omitempty"`
		SkipReason    string         `json:"skip_reason,omitempty"`
		StrategyKind  string         `json:"strategy_kind,omitempty"`
		MutationCount int            `json:"mutation_count,omitempty"`
		StatusCode    int            `json:"status_code"`
		DurationMS    int64          `json:"duration_ms"`
		Failures      []Failure      `json:"failures,omitempty"`
		Request       RequestDetail  `json:"request"`
		Response      ResponseDetail `json:"response"`
		ArtifactPath  string         `json:"artifact_path,omitempty"`
	}
	return json.Marshal(out{
		CaseID:        r.CaseID,
		Passed:        r.Passed,
		Skipped:       r.Skipped,
		SkipReason:    r.SkipReason,
		StrategyKind:  r.StrategyKind,
		MutationCount: r.MutationCount,
		StatusCode:    r.StatusCode,
		DurationMS:    r.Duration.Milliseconds(),
		Failures:      r.Failures,
		Request:       r.Request,
		Response:      r.Response,
		ArtifactPath:  r.ArtifactPath,
	})
}

func (r *Result) UnmarshalJSON(data []byte) error {
	type in struct {
		CaseID        string         `json:"case_id"`
		Passed        bool           `json:"passed"`
		Skipped       bool           `json:"skipped,omitempty"`
		SkipReason    string         `json:"skip_reason,omitempty"`
		StrategyKind  string         `json:"strategy_kind,omitempty"`
		MutationCount int            `json:"mutation_count,omitempty"`
		StatusCode    int            `json:"status_code"`
		DurationMS    int64          `json:"duration_ms"`
		Failures      []Failure      `json:"failures,omitempty"`
		Request       RequestDetail  `json:"request"`
		Response      ResponseDetail `json:"response"`
		ArtifactPath  string         `json:"artifact_path,omitempty"`
	}
	var aux in
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.CaseID = aux.CaseID
	r.Passed = aux.Passed
	r.Skipped = aux.Skipped
	r.SkipReason = aux.SkipReason
	r.StrategyKind = aux.StrategyKind
	r.MutationCount = aux.MutationCount
	r.StatusCode = aux.StatusCode
	r.Duration = time.Duration(aux.DurationMS) * time.Millisecond
	r.Failures = aux.Failures
	r.Request = aux.Request
	r.Response = aux.Response
	r.ArtifactPath = aux.ArtifactPath
	return nil
}
