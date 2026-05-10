package model

// GQLOperationKind classifies a GraphQL root field operation.
type GQLOperationKind string

const (
	GQLQuery        GQLOperationKind = "Query"
	GQLMutation     GQLOperationKind = "Mutation"
	GQLSubscription GQLOperationKind = "Subscription"
)

type Operation struct {
	ID          string         `json:"id"`
	Method      string         `json:"method"`
	Path        string         `json:"path"`
	Tags        []string       `json:"tags,omitempty"`
	Parameters  []Parameter    `json:"parameters,omitempty"`
	RequestBody *SchemaRef     `json:"request_body,omitempty"`
	Responses   []ResponseSpec `json:"responses,omitempty"`

	GQLKind              GQLOperationKind       `json:"gql_kind,omitempty"`
	GQLDocument          string                 `json:"gql_document,omitempty"`
	GQLVariableTypes     map[string]*SchemaRef  `json:"gql_variable_types,omitempty"`
	GQLSelectionSchema   *SchemaRef             `json:"gql_selection_schema,omitempty"`
}

type Parameter struct {
	Name     string     `json:"name"`
	In       string     `json:"in"`
	Required bool       `json:"required"`
	Schema   *SchemaRef `json:"schema,omitempty"`
}

type SchemaRef struct {
	Type       string                `json:"type,omitempty"`
	Format     string                `json:"format,omitempty"`
	Properties map[string]*SchemaRef `json:"properties,omitempty"`
	Items      *SchemaRef            `json:"items,omitempty"`
	// AdditionalProperties, when non-nil, mirrors OpenAPI: false rejects undeclared object keys (contract strategy records warnings).
	AdditionalProperties *bool        `json:"additionalProperties,omitempty"`
	Required             []string     `json:"required,omitempty"`
	Nullable             bool         `json:"nullable,omitempty"`
	Enum                 []any        `json:"enum,omitempty"`
	OneOf                []*SchemaRef `json:"oneOf,omitempty"`
	AnyOf                []*SchemaRef `json:"anyOf,omitempty"`
	MinLength            *int         `json:"minLength,omitempty"`
	MaxLength            *int         `json:"maxLength,omitempty"`
	Minimum              *float64     `json:"minimum,omitempty"`
	Maximum              *float64     `json:"maximum,omitempty"`
	MinItems             *int         `json:"minItems,omitempty"`
	MaxItems             *int         `json:"maxItems,omitempty"`
}

type ResponseSpec struct {
	StatusCode int        `json:"status_code"`
	Schema     *SchemaRef `json:"schema,omitempty"`
}
