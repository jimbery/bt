package model

import "strings"

type Case struct {
	ID                string           `json:"id"`
	OperationID       string           `json:"operation_id"`
	Input             CaseInput        `json:"input"`
	Expected          *CaseExpectation `json:"expected,omitempty"`
	Meta              map[string]any   `json:"meta,omitempty"`
	ResolvedOperation *Operation       `json:"-"` // set by CLI before Execute for GraphQL-aware validation
}

type CaseInput struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Query   map[string]string `json:"query,omitempty"`
	Body    any               `json:"body,omitempty"`

	GQLQuery         string         `json:"gql_query,omitempty"`
	GQLOperationName string         `json:"gql_operation_name,omitempty"`
	GQLVariables     map[string]any `json:"gql_variables,omitempty"`
}

// IsGraphQL reports whether this input represents a GraphQL operation over HTTP.
func (c CaseInput) IsGraphQL() bool { return strings.TrimSpace(c.GQLQuery) != "" }

type CaseExpectation struct {
	StatusCode int               `json:"status_code,omitempty"`
	Schema     *SchemaRef        `json:"schema,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`

	// GraphQL table expectations (YAML gql_*). Used only by the table strategy.
	GQLData       map[string]any `json:"gql_data,omitempty"`
	GQLNoErrors   *bool          `json:"gql_no_errors,omitempty"`
	GQLHasErrors  *bool          `json:"gql_has_errors,omitempty"`
	GQLDataSchema *SchemaRef     `json:"gql_data_schema,omitempty"`
}
