package model

type Case struct {
	ID          string           `json:"id"`
	OperationID string           `json:"operation_id"`
	Input       CaseInput        `json:"input"`
	Expected    *CaseExpectation `json:"expected,omitempty"`
	Meta        map[string]any   `json:"meta,omitempty"`
}

type CaseInput struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Body    any               `json:"body,omitempty"`
}

type CaseExpectation struct {
	StatusCode int               `json:"status_code,omitempty"`
	Schema     *SchemaRef        `json:"schema,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
}
