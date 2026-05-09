package model

type Operation struct {
	ID          string         `json:"id"`
	Method      string         `json:"method"`
	Path        string         `json:"path"`
	Tags        []string       `json:"tags,omitempty"`
	Parameters  []Parameter    `json:"parameters,omitempty"`
	RequestBody *SchemaRef     `json:"request_body,omitempty"`
	Responses   []ResponseSpec `json:"responses,omitempty"`
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
	Required   []string              `json:"required,omitempty"`
	Nullable   bool                  `json:"nullable,omitempty"`
	Enum       []any                 `json:"enum,omitempty"`
	OneOf      []*SchemaRef          `json:"oneOf,omitempty"`
	AnyOf      []*SchemaRef          `json:"anyOf,omitempty"`
}

type ResponseSpec struct {
	StatusCode int        `json:"status_code"`
	Schema     *SchemaRef `json:"schema,omitempty"`
}
