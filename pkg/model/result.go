package model

import (
	"encoding/json"
	"time"
)

type Result struct {
	CaseID     string         `json:"case_id"`
	Passed     bool           `json:"passed"`
	StatusCode int            `json:"status_code"`
	Duration   time.Duration  `json:"-"`
	Failures   []Failure      `json:"failures,omitempty"`
	Request    RequestDetail  `json:"request"`
	Response   ResponseDetail `json:"response"`
}

type Failure struct {
	Invariant string `json:"invariant"`
	Message   string `json:"message"`
	Expected  any    `json:"expected,omitempty"`
	Actual    any    `json:"actual,omitempty"`
}

type RequestDetail struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    []byte            `json:"body,omitempty"`
}

type ResponseDetail struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers,omitempty"`
	Body       []byte            `json:"body,omitempty"`
}

func (r Result) MarshalJSON() ([]byte, error) {
	type out struct {
		CaseID     string         `json:"case_id"`
		Passed     bool           `json:"passed"`
		StatusCode int            `json:"status_code"`
		DurationMS int64          `json:"duration_ms"`
		Failures   []Failure      `json:"failures,omitempty"`
		Request    RequestDetail  `json:"request"`
		Response   ResponseDetail `json:"response"`
	}
	return json.Marshal(out{
		CaseID:     r.CaseID,
		Passed:     r.Passed,
		StatusCode: r.StatusCode,
		DurationMS: r.Duration.Milliseconds(),
		Failures:   r.Failures,
		Request:    r.Request,
		Response:   r.Response,
	})
}

func (r *Result) UnmarshalJSON(data []byte) error {
	type in struct {
		CaseID     string         `json:"case_id"`
		Passed     bool           `json:"passed"`
		StatusCode int            `json:"status_code"`
		DurationMS int64          `json:"duration_ms"`
		Failures   []Failure      `json:"failures,omitempty"`
		Request    RequestDetail  `json:"request"`
		Response   ResponseDetail `json:"response"`
	}
	var aux in
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.CaseID = aux.CaseID
	r.Passed = aux.Passed
	r.StatusCode = aux.StatusCode
	r.Duration = time.Duration(aux.DurationMS) * time.Millisecond
	r.Failures = aux.Failures
	r.Request = aux.Request
	r.Response = aux.Response
	return nil
}
