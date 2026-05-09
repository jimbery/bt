package model

type Target struct {
	Name          string     `json:"name"`
	BaseURL       string     `json:"base_url"`
	SchemaPath    string     `json:"schema_path"`
	Environment   string     `json:"environment,omitempty"`
	Auth          AuthConfig `json:"auth"`
}

type AuthConfig struct {
	Type string `json:"type"`
	Env  string `json:"env"`
}
