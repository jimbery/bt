package model

type Invariant struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config,omitempty"`
}
