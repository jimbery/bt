// Package invariant implements GraphQL-specific property invariants.
package invariant

import (
	"encoding/json"
	"strings"

	gqlassert "github.com/jimbery/bt/internal/strategy/graphql/assert"
	"github.com/jimbery/bt/pkg/model"
)

// NoGQLErrorsConfig configures the no_gql_errors invariant.
type NoGQLErrorsConfig struct {
	Severity model.ViolationSeverity
}

// EvaluateNoGQLErrors fails when the GraphQL response contains a non-empty errors array.
func EvaluateNoGQLErrors(cfg NoGQLErrorsConfig, op model.Operation, res model.Result) []model.Failure {
	_ = op
	out := []model.Failure{}
	sev := cfg.Severity
	if sev == "" {
		sev = model.SeverityCritical
	}
	var root map[string]any
	if err := json.Unmarshal(res.Response.Body, &root); err != nil {
		return append(out, model.Failure{
			Invariant: model.InvariantNoGQLErrors,
			Message:   "response body is not valid JSON: " + err.Error(),
			Path:      "body",
		})
	}
	errsRaw, has := root["errors"]
	if !has || errsRaw == nil {
		return out
	}
	errs, ok := errsRaw.([]any)
	if !ok || len(errs) == 0 {
		return out
	}

	msg := firstGraphQLErrorMessage(errs)
	if msg == "" {
		msg = "GraphQL errors present"
	}

	class := ""
	if sev == model.SeverityWarning {
		class = "graphql_warning"
	}
	return append(out, model.Failure{
		Invariant:      model.InvariantNoGQLErrors,
		Message:        msg,
		Path:           "errors",
		Classification: class,
	})
}

func firstGraphQLErrorMessage(errs []any) string {
	for _, ei := range errs {
		em, ok := ei.(map[string]any)
		if !ok {
			continue
		}
		if m, ok := em["message"].(string); ok && strings.TrimSpace(m) != "" {
			return m
		}
	}
	return ""
}

// EvaluateResponseMatchesSelection validates data.* against GQLSelectionSchema (selection only).
func EvaluateResponseMatchesSelection(op model.Operation, res model.Result) []model.Failure {
	var out []model.Failure
	for _, af := range gqlassert.AssertSelectionSchema(res.Response.Body, op) {
		out = append(out, assertionFailureToModel(model.InvariantResponseMatchesSchema, af))
	}
	return out
}

func assertionFailureToModel(inv string, af gqlassert.AssertionFailure) model.Failure {
	class := ""
	if af.Severity == gqlassert.Warning {
		class = "graphql_warning"
	}
	return model.Failure{
		Invariant:      inv,
		Message:        af.Message,
		Path:           af.Field,
		Classification: class,
	}
}

// NoGQLErrorsConfigFromInvariant reads optional severity from model.Invariant.Config.
func NoGQLErrorsConfigFromInvariant(inv model.Invariant) NoGQLErrorsConfig {
	var cfg NoGQLErrorsConfig
	if inv.Config == nil {
		return cfg
	}
	if s, ok := inv.Config["severity"].(string); ok {
		cfg.Severity = model.ViolationSeverity(s)
	}
	return cfg
}
