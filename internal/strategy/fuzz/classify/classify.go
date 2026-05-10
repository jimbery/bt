// Package classify maps HTTP outcomes to fuzz failure classes.
package classify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jayimbery/bt/internal/strategy/property/validate"
	"github.com/jayimbery/bt/pkg/model"
)

// ErrTimeout is a sentinel recognised as a request timeout (tests and callers).
var ErrTimeout = errors.New("timeout")

// Classification describes how the server behaved under fuzz input.
type Classification string

const (
	ClassificationCrash            Classification = "crash"
	ClassificationTimeout          Classification = "timeout"
	ClassificationValidationLeak   Classification = "validation_leak"
	ClassificationSchemaBreak      Classification = "schema_break"
	ClassificationUnexpectedStatus Classification = "unexpected_status"
	ClassificationPass             Classification = "pass"
)

// Classify applies rules in priority order. It never panics.
func Classify(resp *http.Response, body []byte, err error, op model.Operation) Classification {
	status := 0
	if resp != nil {
		status = resp.StatusCode
	}
	return classifyCore(status, body, err, op)
}

// ClassifyDetail classifies using a runner ResponseDetail (no *http.Response required).
func ClassifyDetail(d model.ResponseDetail, err error, op model.Operation) Classification {
	return classifyCore(d.StatusCode, d.Body, err, op)
}

func classifyCore(status int, body []byte, err error, op model.Operation) Classification {
	if err != nil {
		if errors.Is(err, ErrTimeout) || errors.Is(err, context.DeadlineExceeded) {
			return ClassificationTimeout
		}
		return ClassificationCrash
	}
	if status == 500 && isPlainStackTrace(body) {
		return ClassificationCrash
	}
	if hasValidationLeak(body) {
		return ClassificationValidationLeak
	}
	if sch := schemaForStatus(op, status); sch != nil {
		if v := validate.ValidateResponse(body, sch); len(v) > 0 {
			return ClassificationSchemaBreak
		}
	}
	if len(op.Responses) > 0 && !statusDeclared(op, status) {
		// GraphQL is always POST to a single path; path/query fuzz mutations often yield
		// 404/405 that are not modeled as alternate response codes on the operation.
		if strings.TrimSpace(op.GQLDocument) != "" && (status == http.StatusNotFound || status == http.StatusMethodNotAllowed || status == http.StatusBadRequest || status == http.StatusUnsupportedMediaType) {
			return ClassificationPass
		}
		return ClassificationUnexpectedStatus
	}
	return ClassificationPass
}

func schemaForStatus(op model.Operation, code int) *model.SchemaRef {
	for _, r := range op.Responses {
		if r.StatusCode == code {
			return r.Schema
		}
	}
	return nil
}

func statusDeclared(op model.Operation, code int) bool {
	for _, r := range op.Responses {
		if r.StatusCode == code {
			return true
		}
	}
	return false
}

func isPlainStackTrace(body []byte) bool {
	s := strings.TrimSpace(string(body))
	if s == "" || s[0] == '{' {
		return false
	}
	return strings.Contains(s, "goroutine") &&
		(strings.Contains(s, "runtime error:") || strings.Contains(s, "[running]"))
}

func hasValidationLeak(body []byte) bool {
	s := strings.ToLower(string(body))
	if s == "" {
		return false
	}
	patterns := []string{
		"goroutine ",
		"/home/",
		"/var/",
		`c:\`,
		"runtime error:",
		"sql syntax",
		" pq:",
		"pq:", // lib/pq and similar ("pq: duplicate key ...")
		"mysql:",
		"sqlite3:",
	}
	for _, p := range patterns {
		if strings.Contains(s, strings.ToLower(p)) {
			return true
		}
	}
	return false
}

// Message returns a short human-readable explanation for a classification.
func Message(c Classification, err error) string {
	switch c {
	case ClassificationCrash:
		if err != nil {
			return fmt.Sprintf("transport or server failure: %v", err)
		}
		return "server returned a stack trace-style 500 body"
	case ClassificationTimeout:
		return "request exceeded timeout"
	case ClassificationValidationLeak:
		return "response body appears to leak internal implementation details"
	case ClassificationSchemaBreak:
		return "response body does not match the declared schema for this status code"
	case ClassificationUnexpectedStatus:
		return "HTTP status is not declared for this operation in the OpenAPI document"
	default:
		return "ok"
	}
}
