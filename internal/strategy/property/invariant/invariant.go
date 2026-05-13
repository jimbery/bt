// Package invariant implements property-strategy invariants.
package invariant

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jayimbery/bt/internal/gqlcase"
	gqlinvariant "github.com/jayimbery/bt/internal/strategy/graphql/invariant"
	"github.com/jayimbery/bt/internal/strategy/property/validate"
	"github.com/jayimbery/bt/pkg/model"
)

// No5xx fails when the response status is in the 5xx range.
func No5xx(res model.Result) []model.Failure {
	code := res.Response.StatusCode
	if code < 500 {
		return nil
	}
	return []model.Failure{{
		Invariant: model.InvariantNo5xx,
		Message:   fmt.Sprintf("HTTP status %d is a server error", code),
		Expected:  "< 500",
		Actual:    strconv.Itoa(code),
	}}
}

// ResponseMatchesSchema validates the response body against the operation schema
// for the response status code. Skips when no schema is defined for that status.
// For GraphQL operations with a selection schema, validates the selection shape only.
func ResponseMatchesSchema(op model.Operation, res model.Result) []model.Failure {
	if gqlcase.IsGraphQLOperation(op) && op.GQLSelectionSchema != nil {
		return gqlinvariant.EvaluateResponseMatchesSelection(op, res)
	}
	return responseMatchesOpenAPI(op, res)
}

func responseMatchesOpenAPI(op model.Operation, res model.Result) []model.Failure {
	schema := schemaForStatus(op, res.Response.StatusCode)
	if schema == nil {
		return nil
	}
	violations := validate.ValidateResponse(res.Response.Body, schema)
	if len(violations) == 0 {
		return nil
	}
	out := make([]model.Failure, 0, len(violations))
	for _, v := range violations {
		out = append(out, model.Failure{
			Invariant: model.InvariantResponseMatchesSchema,
			Message:   v.Message,
			Path:      v.Path,
			Expected:  v.Expected,
			Actual:    v.Got,
		})
	}
	return out
}

func schemaForStatus(op model.Operation, code int) *model.SchemaRef {
	for _, r := range op.Responses {
		if r.StatusCode == code {
			return r.Schema
		}
	}
	return nil
}

// IdempotencyKeyPrevents compares two responses for the same idempotency key.
func IdempotencyKeyPrevents(ir model.IdempotencyResult) []model.Failure {
	if ir.IdempotencyKey == "" {
		return nil
	}
	var out []model.Failure
	if ir.First.StatusCode != ir.Second.StatusCode {
		out = append(out, model.Failure{
			Invariant: model.InvariantIdempotencyKeyPreventsDupes,
			Message:   "idempotent replay returned different status codes",
			Expected:  strconv.Itoa(ir.First.StatusCode),
			Actual:    strconv.Itoa(ir.Second.StatusCode),
		})
	}
	if !bytes.Equal(bytes.TrimSpace(ir.First.Body), bytes.TrimSpace(ir.Second.Body)) {
		out = append(out, model.Failure{
			Invariant: model.InvariantIdempotencyKeyPreventsDupes,
			Message:   "idempotent replay returned different response bodies",
			Expected:  string(ir.First.Body),
			Actual:    string(ir.Second.Body),
		})
	}
	return out
}

// PropertyEval evaluates invariants that need operation context.
type PropertyEval func(inv model.Invariant, op model.Operation, res model.Result, idem *model.IdempotencyResult) []model.Failure

// Lookup returns a registered property invariant by config name.
func Lookup(name string) (PropertyEval, bool) {
	fn, ok := registry[name]
	return fn, ok
}

var registry = map[string]PropertyEval{
	model.InvariantNo5xx: func(_ model.Invariant, op model.Operation, res model.Result, _ *model.IdempotencyResult) []model.Failure {
		_ = op
		return No5xx(res)
	},
	model.InvariantResponseMatchesSchema: func(_ model.Invariant, op model.Operation, res model.Result, _ *model.IdempotencyResult) []model.Failure {
		return ResponseMatchesSchema(op, res)
	},
	model.InvariantNoGQLErrors: func(inv model.Invariant, op model.Operation, res model.Result, _ *model.IdempotencyResult) []model.Failure {
		cfg := gqlinvariant.NoGQLErrorsConfigFromInvariant(inv)
		return gqlinvariant.EvaluateNoGQLErrors(cfg, op, res)
	},
	model.InvariantIdempotencyKeyPreventsDupes: func(_ model.Invariant, _ model.Operation, _ model.Result, idem *model.IdempotencyResult) []model.Failure {
		if idem == nil {
			return nil
		}
		return IdempotencyKeyPrevents(*idem)
	},
}

// IdempotencyHeader is the canonical header name for idempotency keys.
const IdempotencyHeader = "Idempotency-Key"

// HeaderKey returns the canonical HTTP header key for idempotency.
func HeaderKey() string {
	return http.CanonicalHeaderKey(IdempotencyHeader)
}
