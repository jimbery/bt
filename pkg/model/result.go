package model

import (
	"encoding/json"
	"strings"
	"time"
)

type Result struct {
	CaseID       string `json:"case_id"`
	OperationID  string `json:"operation_id,omitempty"`
	Passed       bool   `json:"passed"`
	Skipped      bool   `json:"skipped,omitempty"`
	SkipReason   string `json:"skip_reason,omitempty"`
	StrategyKind string `json:"strategy_kind,omitempty"`
	// ContractSchemaRef is set by the contract strategy when a response schema was evaluated.
	ContractSchemaRef string `json:"contract_schema_ref,omitempty"`
	// Quarantined is set when a failing contract result matches an active baseline entry.
	Quarantined       bool           `json:"quarantined,omitempty"`
	QuarantineReason  string         `json:"quarantine_reason,omitempty"`
	QuarantineExpired bool           `json:"quarantine_expired,omitempty"`
	StaleBaseline     bool           `json:"stale_baseline,omitempty"`
	MutationCount     int            `json:"mutation_count,omitempty"`
	Seed              int64          `json:"seed,omitempty"`
	CasesRun          int            `json:"cases_run,omitempty"`
	ShrinkCount       int            `json:"shrink_count,omitempty"`
	StatusCode        int            `json:"status_code"`
	Duration          time.Duration  `json:"-"`
	Failures          []Failure      `json:"failures,omitempty"`
	Request           RequestDetail  `json:"request"`
	Response          ResponseDetail `json:"response"`
	ArtifactPath      string         `json:"artifact_path,omitempty"`
	// SchemaViolations lists response body schema disagreements (table strategy, etc.).
	// Encoding always uses a JSON array, never null.
	SchemaViolations []SchemaViolation `json:"schema_violations,omitempty"`
	// StatefulResult is set when strategy_kind is stateful (M13).
	StatefulResult *FlowResult `json:"stateful_result,omitempty"`
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
		var probe map[string]any
		if err := json.Unmarshal(r.Body, &probe); err == nil {
			if q, ok := probe["query"].(string); ok && strings.TrimSpace(q) != "" {
				ci.GQLQuery = q
				if on, ok := probe["operationName"].(string); ok {
					ci.GQLOperationName = on
				}
				if vars, ok := probe["variables"].(map[string]any); ok {
					ci.GQLVariables = vars
				}
				return ci
			}
		}
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
		CaseID            string            `json:"case_id"`
		OperationID       string            `json:"operation_id,omitempty"`
		Passed            bool              `json:"passed"`
		Skipped           bool              `json:"skipped,omitempty"`
		SkipReason        string            `json:"skip_reason,omitempty"`
		StrategyKind      string            `json:"strategy_kind,omitempty"`
		ContractSchemaRef string            `json:"contract_schema_ref,omitempty"`
		Quarantined       bool              `json:"quarantined,omitempty"`
		QuarantineReason  string            `json:"quarantine_reason,omitempty"`
		QuarantineExpired bool              `json:"quarantine_expired,omitempty"`
		StaleBaseline     bool              `json:"stale_baseline,omitempty"`
		MutationCount     int               `json:"mutation_count,omitempty"`
		Seed              int64             `json:"seed,omitempty"`
		CasesRun          int               `json:"cases_run,omitempty"`
		ShrinkCount       int               `json:"shrink_count,omitempty"`
		StatusCode        int               `json:"status_code"`
		DurationMS        int64             `json:"duration_ms"`
		Failures          []Failure         `json:"failures,omitempty"`
		Request           RequestDetail     `json:"request"`
		Response          ResponseDetail    `json:"response"`
		ArtifactPath      string            `json:"artifact_path,omitempty"`
		SchemaViolations  []SchemaViolation `json:"schema_violations"`
		StatefulResult    *FlowResult       `json:"stateful_result,omitempty"`
	}
	sv := r.SchemaViolations
	if sv == nil {
		sv = []SchemaViolation{}
	}
	return json.Marshal(out{
		CaseID:            r.CaseID,
		OperationID:       r.OperationID,
		Passed:            r.Passed,
		Skipped:           r.Skipped,
		SkipReason:        r.SkipReason,
		StrategyKind:      r.StrategyKind,
		ContractSchemaRef: r.ContractSchemaRef,
		Quarantined:       r.Quarantined,
		QuarantineReason:  r.QuarantineReason,
		QuarantineExpired: r.QuarantineExpired,
		StaleBaseline:     r.StaleBaseline,
		MutationCount:     r.MutationCount,
		Seed:              r.Seed,
		CasesRun:          r.CasesRun,
		ShrinkCount:       r.ShrinkCount,
		StatusCode:        r.StatusCode,
		DurationMS:        r.Duration.Milliseconds(),
		Failures:          r.Failures,
		Request:           r.Request,
		Response:          r.Response,
		ArtifactPath:      r.ArtifactPath,
		SchemaViolations:  sv,
		StatefulResult:    r.StatefulResult,
	})
}

func (r *Result) UnmarshalJSON(data []byte) error {
	type in struct {
		CaseID            string            `json:"case_id"`
		OperationID       string            `json:"operation_id,omitempty"`
		Passed            bool              `json:"passed"`
		Skipped           bool              `json:"skipped,omitempty"`
		SkipReason        string            `json:"skip_reason,omitempty"`
		StrategyKind      string            `json:"strategy_kind,omitempty"`
		ContractSchemaRef string            `json:"contract_schema_ref,omitempty"`
		Quarantined       bool              `json:"quarantined,omitempty"`
		QuarantineReason  string            `json:"quarantine_reason,omitempty"`
		QuarantineExpired bool              `json:"quarantine_expired,omitempty"`
		StaleBaseline     bool              `json:"stale_baseline,omitempty"`
		MutationCount     int               `json:"mutation_count,omitempty"`
		Seed              int64             `json:"seed,omitempty"`
		CasesRun          int               `json:"cases_run,omitempty"`
		ShrinkCount       int               `json:"shrink_count,omitempty"`
		StatusCode        int               `json:"status_code"`
		DurationMS        int64             `json:"duration_ms"`
		Failures          []Failure         `json:"failures,omitempty"`
		Request           RequestDetail     `json:"request"`
		Response          ResponseDetail    `json:"response"`
		ArtifactPath      string            `json:"artifact_path,omitempty"`
		SchemaViolations  []SchemaViolation `json:"schema_violations"`
		StatefulResult    *FlowResult       `json:"stateful_result,omitempty"`
	}
	var aux in
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.CaseID = aux.CaseID
	r.OperationID = aux.OperationID
	r.Passed = aux.Passed
	r.Skipped = aux.Skipped
	r.SkipReason = aux.SkipReason
	r.StrategyKind = aux.StrategyKind
	r.ContractSchemaRef = aux.ContractSchemaRef
	r.Quarantined = aux.Quarantined
	r.QuarantineReason = aux.QuarantineReason
	r.QuarantineExpired = aux.QuarantineExpired
	r.StaleBaseline = aux.StaleBaseline
	r.MutationCount = aux.MutationCount
	r.Seed = aux.Seed
	r.CasesRun = aux.CasesRun
	r.ShrinkCount = aux.ShrinkCount
	r.StatusCode = aux.StatusCode
	r.Duration = time.Duration(aux.DurationMS) * time.Millisecond
	r.Failures = aux.Failures
	r.Request = aux.Request
	r.Response = aux.Response
	r.ArtifactPath = aux.ArtifactPath
	r.SchemaViolations = aux.SchemaViolations
	r.StatefulResult = aux.StatefulResult
	if r.SchemaViolations == nil {
		r.SchemaViolations = []SchemaViolation{}
	}
	return nil
}
