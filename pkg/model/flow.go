package model

import (
	"encoding/json"
	"net/http"
	"strings"
)

// InlineSchema is an OpenAPI-shaped JSON schema fragment embedded in a flow step (M13).
type InlineSchema = SchemaRef

// Flow is a named multi-step scenario (M13 stateful strategy).
type Flow struct {
	ID          string     `json:"id"`
	Description string     `json:"description,omitempty"`
	Steps       []FlowStep `json:"steps"`
}

// FlowStep is one HTTP operation in a flow with optional extraction rules.
type FlowStep struct {
	ID          string                 `json:"id"`
	OperationID string                 `json:"operation_id"`
	Input       StepInput              `json:"input"`
	Expected    *StepExpectation       `json:"expected,omitempty"`
	Extract     map[string]ExtractSpec `json:"extract,omitempty"`
}

// StepInput is the logical HTTP request for a flow step (before binding injection).
type StepInput struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Body    any               `json:"body,omitempty"`
}

// StepExpectation is checked after each step; failures do not halt the flow (M13).
type StepExpectation struct {
	StatusCode int        `json:"status_code,omitempty"`
	Schema     *SchemaRef `json:"schema,omitempty"`
}

// ExtractSpec describes how to read a value from a response and where it may be injected later.
type ExtractSpec struct {
	From string `json:"from"`
	Into string `json:"into"`
}

// FlowResult is the outcome of executing one flow.
type FlowResult struct {
	FlowID       string       `json:"flow_id"`
	Passed       bool         `json:"passed"`
	Steps        []StepResult `json:"steps"`
	ArtifactPath string       `json:"artifact_path,omitempty"`
}

// StepResult captures one executed step including bindings and optional failures.
type StepResult struct {
	StepID           string            `json:"step_id"`
	OperationID      string            `json:"operation_id"`
	Passed           bool              `json:"passed"`
	StatusCode       int               `json:"status_code"`
	Request          ResolvedRequest   `json:"request"`
	Response         StepResponse      `json:"response"`
	Bindings         map[string]any    `json:"bindings,omitempty"`
	SchemaViolations []SchemaViolation `json:"schema_violations"`
	BindingFailure   *BindingFailure   `json:"binding_failure,omitempty"`
}

// MarshalJSON ensures schema_violations is always a JSON array, never null.
func (sr StepResult) MarshalJSON() ([]byte, error) {
	type out struct {
		StepID           string            `json:"step_id"`
		OperationID      string            `json:"operation_id"`
		Passed           bool              `json:"passed"`
		StatusCode       int               `json:"status_code"`
		Request          ResolvedRequest   `json:"request"`
		Response         StepResponse      `json:"response"`
		Bindings         map[string]any    `json:"bindings,omitempty"`
		SchemaViolations []SchemaViolation `json:"schema_violations"`
		BindingFailure   *BindingFailure   `json:"binding_failure,omitempty"`
	}
	sv := sr.SchemaViolations
	if sv == nil {
		sv = []SchemaViolation{}
	}
	return json.Marshal(out{
		StepID:           sr.StepID,
		OperationID:      sr.OperationID,
		Passed:           sr.Passed,
		StatusCode:       sr.StatusCode,
		Request:          sr.Request,
		Response:         sr.Response,
		Bindings:         sr.Bindings,
		SchemaViolations: sv,
		BindingFailure:   sr.BindingFailure,
	})
}

// UnmarshalJSON keeps schema_violations as non-nil slice after decode.
func (sr *StepResult) UnmarshalJSON(data []byte) error {
	type stepResultJSON StepResult
	var aux stepResultJSON
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*sr = StepResult(aux)
	if sr.SchemaViolations == nil {
		sr.SchemaViolations = []SchemaViolation{}
	}
	return nil
}

// BindingFailure records a fatal extraction error (halts the flow).
type BindingFailure struct {
	Key          string `json:"key"`
	Expression   string `json:"expression"`
	Severity     string `json:"severity"`
	Message      string `json:"message"`
	ResponseBody []byte `json:"response_body,omitempty"`
}

// ResolvedRequest is the concrete HTTP request after binding injection.
type ResolvedRequest struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Headers     http.Header       `json:"headers"`
	QueryParams map[string]string `json:"query_params,omitempty"`
	Body        []byte            `json:"body,omitempty"`
}

// AsCaseInput converts a resolved request into CaseInput for HTTP replay.
func (r ResolvedRequest) AsCaseInput() CaseInput {
	hdr := map[string]string{}
	for k, vs := range r.Headers {
		if len(vs) > 0 {
			hdr[k] = vs[0]
		}
	}
	ci := CaseInput{
		Method:  r.Method,
		Path:    r.Path,
		Headers: hdr,
		Query:   r.QueryParams,
	}
	if len(r.Body) == 0 {
		return ci
	}
	body := append([]byte(nil), r.Body...)
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err == nil {
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
	if err := json.Unmarshal(body, &v); err == nil {
		ci.Body = v
	}
	return ci
}

// StepResponse is the HTTP response for a flow step.
type StepResponse struct {
	StatusCode int         `json:"status_code"`
	Headers    http.Header `json:"headers"`
	Body       []byte      `json:"body,omitempty"`
}
