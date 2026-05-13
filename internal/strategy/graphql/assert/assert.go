package assert

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jayimbery/bt/internal/strategy/contract"
	"github.com/jayimbery/bt/pkg/model"
)

// AssertionSeverity classifies GraphQL assertion outcomes.
type AssertionSeverity int

const (
	Critical AssertionSeverity = iota
	Warning
)

// AssertionFailure records one GraphQL response assertion outcome.
type AssertionFailure struct {
	Field    string
	Message  string
	Severity AssertionSeverity
}

// AssertResponse validates a GraphQL HTTP response body against the operation's schema hints.
func AssertResponse(body []byte, op model.Operation) []AssertionFailure {
	var out []AssertionFailure
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return append(out, AssertionFailure{Field: "body", Message: "invalid JSON", Severity: Critical})
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return append(out, AssertionFailure{Field: "body", Message: "expected top-level JSON object", Severity: Critical})
	}
	if _, has := obj["data"]; !has {
		return append(out, AssertionFailure{Field: "data", Message: `response must include a "data" key`, Severity: Critical})
	}
	dataVal := obj["data"]

	if errsRaw, has := obj["errors"]; has && errsRaw != nil {
		if errs, ok := errsRaw.([]any); ok && len(errs) > 0 {
			valid := true
			for i, ei := range errs {
				em, ok := ei.(map[string]any)
				if !ok {
					valid = false
					out = append(out, AssertionFailure{
						Field:    fmt.Sprintf("errors[%d]", i),
						Message:  "each error must be an object",
						Severity: Critical,
					})
					continue
				}
				msg, ok := em["message"].(string)
				if !ok || msg == "" {
					valid = false
					out = append(out, AssertionFailure{
						Field:    fmt.Sprintf("errors[%d].message", i),
						Message:  `each error must include a string "message"`,
						Severity: Critical,
					})
				}
			}
			if valid {
				if dataVal == nil {
					out = append(out, AssertionFailure{
						Field:    "data",
						Message:  "data is null while errors are present",
						Severity: Critical,
					})
				} else {
					out = append(out, AssertionFailure{
						Field:    "errors",
						Message:  "GraphQL response includes errors alongside data",
						Severity: Warning,
					})
				}
			}
		}
	}

	if op.GQLSelectionSchema != nil && dataVal != nil {
		payload := graphqlDataPayload(dataVal, op.ID)
		if payload == nil {
			return out
		}
		switch v := payload.(type) {
		case map[string]any:
			for _, cv := range contract.EvaluateBody(v, op.GQLSelectionSchema) {
				out = append(out, contractViolationToFailure(cv))
			}
		default:
			raw, err := json.Marshal(v)
			if err != nil {
				out = append(out, AssertionFailure{Field: "data", Message: err.Error(), Severity: Critical})
				break
			}
			for _, cv := range contract.EvaluateJSON(raw, op.GQLSelectionSchema) {
				out = append(out, contractViolationToFailure(cv))
			}
		}
	}

	return out
}

// AssertSelectionSchema validates only the GraphQL selection shape (GQLSelectionSchema)
// against the operation payload under data.*. It does not inspect the errors array.
func AssertSelectionSchema(body []byte, op model.Operation) []AssertionFailure {
	var out []AssertionFailure
	if op.GQLSelectionSchema == nil {
		return out
	}
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return append(out, AssertionFailure{Field: "body", Message: "invalid JSON", Severity: Critical})
	}
	obj, ok := root.(map[string]any)
	if !ok {
		return append(out, AssertionFailure{Field: "body", Message: "expected top-level JSON object", Severity: Critical})
	}
	if _, has := obj["data"]; !has {
		return append(out, AssertionFailure{Field: "data", Message: `response must include a "data" key`, Severity: Critical})
	}
	dataVal := obj["data"]

	if dataVal != nil {
		payload := graphqlDataPayload(dataVal, op.ID)
		if payload == nil {
			return out
		}
		switch v := payload.(type) {
		case map[string]any:
			for _, cv := range contract.EvaluateBody(v, op.GQLSelectionSchema) {
				out = append(out, contractViolationToFailure(cv))
			}
		default:
			raw, err := json.Marshal(v)
			if err != nil {
				out = append(out, AssertionFailure{Field: "data", Message: err.Error(), Severity: Critical})
				break
			}
			for _, cv := range contract.EvaluateJSON(raw, op.GQLSelectionSchema) {
				out = append(out, contractViolationToFailure(cv))
			}
		}
	}

	return out
}

func graphqlDataPayload(dataVal any, opID string) any {
	m, ok := dataVal.(map[string]any)
	if !ok || opID == "" {
		return dataVal
	}
	if inner, has := m[opID]; has {
		return inner
	}
	return dataVal
}

func contractViolationToFailure(v contract.ContractViolation) AssertionFailure {
	sev := Critical
	if v.Severity == contract.Warning {
		sev = Warning
	}
	field := v.Field
	if field != "" && field != "body" && field != "$" && !strings.HasPrefix(field, "data.") {
		field = "data." + field
	}
	if field == "body" || field == "$" || field == "" {
		field = "data"
	}
	msg := strings.TrimSpace(v.Expected + ": " + v.Actual)
	if msg == ":" {
		msg = "schema violation"
	}
	return AssertionFailure{Field: field, Message: msg, Severity: sev}
}
