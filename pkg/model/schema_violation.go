package model

import "encoding/json"

// ViolationSeverity classifies how serious a schema disagreement is.
type ViolationSeverity string

const (
	SeverityCritical ViolationSeverity = "Critical"
	SeverityWarning  ViolationSeverity = "Warning"
)

// MarshalJSON emits the severity as a JSON string (e.g. "Critical").
func (s ViolationSeverity) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(s))
}

// SchemaViolation records a single disagreement between a JSON value and a schema.
type SchemaViolation struct {
	Field        string            `json:"field"`
	ExpectedType string            `json:"expected_type,omitempty"`
	ActualType   string            `json:"actual_type,omitempty"`
	Message      string            `json:"message"`
	Severity     ViolationSeverity `json:"severity"`
}

func (v SchemaViolation) String() string {
	return v.Field + ": " + v.Message + " [" + string(v.Severity) + "]"
}
