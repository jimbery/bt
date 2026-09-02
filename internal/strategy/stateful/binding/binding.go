// Package binding implements JSONPath extraction and request injection for stateful flows (M13 / ADR-011).
package binding

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	jsonpath "github.com/PaesslerAG/jsonpath"

	"github.com/jimbery/bt/pkg/model"
)

var (
	ErrBindingNotFound     = errors.New("binding: value not found")
	ErrBindingTypeMismatch = errors.New("binding: type mismatch for injection target")
	ErrConfigError         = errors.New("binding: configuration error")
)

func IsErrBindingNotFound(err error) bool     { return errors.Is(err, ErrBindingNotFound) }
func IsErrBindingTypeMismatch(err error) bool { return errors.Is(err, ErrBindingTypeMismatch) }
func IsErrConfigError(err error) bool         { return errors.Is(err, ErrConfigError) }

// ResolvedInput is the concrete HTTP request after injection.
type ResolvedInput struct {
	Method      string
	Path        string
	Headers     http.Header
	QueryParams map[string]string
	Body        []byte
}

// Extract evaluates an expression against a step response (ADR-011).
func Extract(expr string, resp model.StepResponse) (any, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("%w: empty expression", ErrConfigError)
	}
	switch {
	case strings.EqualFold(expr, "status"):
		return fmt.Sprintf("%d", resp.StatusCode), nil
	case strings.HasPrefix(strings.ToLower(expr), "header."):
		name := expr[len("header."):]
		if resp.Headers == nil {
			return nil, fmt.Errorf("header %s: %w", name, ErrBindingNotFound)
		}
		v := resp.Headers.Get(name)
		if v == "" {
			return nil, fmt.Errorf("header %s: %w", name, ErrBindingNotFound)
		}
		return v, nil
	case expr == "$":
		var m map[string]any
		if err := json.Unmarshal(resp.Body, &m); err != nil {
			return nil, fmt.Errorf("%w: $ requires JSON object body: %v", ErrBindingNotFound, err)
		}
		return m, nil
	default:
		if !strings.HasPrefix(expr, "$") {
			return nil, fmt.Errorf("%w: unsupported expression %q", ErrConfigError, expr)
		}
		var root any
		if err := json.Unmarshal(resp.Body, &root); err != nil {
			return nil, fmt.Errorf("%w: %s: invalid JSON body: %v", ErrBindingNotFound, expr, err)
		}
		v, err := jsonpath.Get(expr, root)
		if err != nil || v == nil {
			return nil, fmt.Errorf("%s: %w", expr, ErrBindingNotFound)
		}
		if arr, ok := v.([]any); ok {
			if len(arr) > 1 {
				return nil, fmt.Errorf("%w: multi-value JSONPath %q", ErrConfigError, expr)
			}
			if len(arr) == 1 {
				return arr[0], nil
			}
			return nil, fmt.Errorf("%s: %w", expr, ErrBindingNotFound)
		}
		if arr, ok := v.([]interface{}); ok {
			if len(arr) > 1 {
				return nil, fmt.Errorf("%w: multi-value JSONPath %q", ErrConfigError, expr)
			}
			if len(arr) == 1 {
				return arr[0], nil
			}
			return nil, fmt.Errorf("%s: %w", expr, ErrBindingNotFound)
		}
		return v, nil
	}
}

// Inject applies bindings to a flow step using Extract metadata on this step.
func Inject(step *model.FlowStep, bindings map[string]any) (*ResolvedInput, error) {
	if step == nil {
		return nil, fmt.Errorf("%w: nil step", ErrConfigError)
	}
	out := &ResolvedInput{
		Method:      strings.TrimSpace(step.Input.Method),
		Path:        step.Input.Path,
		Headers:     http.Header{},
		QueryParams: map[string]string{},
	}
	for k, v := range step.Input.Headers {
		out.Headers.Set(k, v)
	}
	for k, v := range step.Input.Query {
		out.QueryParams[k] = v
	}
	// Validate $ + non-body Into specs on this step
	for key, spec := range step.Extract {
		if strings.TrimSpace(spec.From) == "$" && !strings.EqualFold(strings.TrimSpace(spec.Into), "body") {
			_ = key
			return nil, fmt.Errorf("%w: extract %q uses $ with into %q (only body allowed)", ErrConfigError, key, spec.Into)
		}
	}
	for key, val := range bindings {
		spec, ok := step.Extract[key]
		if !ok {
			continue
		}
		into := strings.TrimSpace(spec.Into)
		switch {
		case strings.EqualFold(into, "path"):
			ph := "{" + key + "}"
			if !strings.Contains(out.Path, ph) {
				continue
			}
			s, err := stringifyPathValue(val)
			if err != nil {
				return nil, err
			}
			out.Path = strings.ReplaceAll(out.Path, ph, s)
		case strings.HasPrefix(strings.ToLower(into), "query."):
			qn := into[len("query."):]
			s, err := stringifyPathValue(val)
			if err != nil {
				return nil, err
			}
			out.QueryParams[qn] = s
		case strings.HasPrefix(strings.ToLower(into), "header."):
			hn := into[len("header."):]
			s, err := stringifyHeaderValue(val)
			if err != nil {
				return nil, err
			}
			out.Headers.Set(hn, s)
		case strings.EqualFold(into, "body"):
			if spec.From != "$" {
				return nil, fmt.Errorf("%w: into body requires from $", ErrConfigError)
			}
			b, err := json.Marshal(val)
			if err != nil {
				return nil, fmt.Errorf("%w: marshal body binding: %w", ErrConfigError, err)
			}
			out.Body = b
		default:
			return nil, fmt.Errorf("%w: unknown into target %q", ErrConfigError, into)
		}
	}
	// Implicit path placeholders for keys not declared on this step
	for key, val := range bindings {
		if _, ok := step.Extract[key]; ok {
			continue
		}
		ph := "{" + key + "}"
		if strings.Contains(out.Path, ph) {
			s, err := stringifyPathValue(val)
			if err != nil {
				return nil, err
			}
			out.Path = strings.ReplaceAll(out.Path, ph, s)
		}
	}
	if step.Input.Body != nil && len(out.Body) == 0 {
		switch b := step.Input.Body.(type) {
		case []byte:
			out.Body = append([]byte(nil), b...)
		default:
			raw, err := json.Marshal(b)
			if err != nil {
				return nil, fmt.Errorf("%w: marshal step body: %w", ErrConfigError, err)
			}
			out.Body = raw
		}
	}
	if len(out.Body) > 0 && len(bindings) > 0 {
		b, err := replaceBodyPlaceholders(out.Body, bindings)
		if err != nil {
			return nil, err
		}
		out.Body = b
	}
	return out, nil
}

// replaceBodyPlaceholders substitutes "{key}" substrings in JSON (e.g. GraphQL variables) from bindings.
func replaceBodyPlaceholders(body []byte, bindings map[string]any) ([]byte, error) {
	if len(body) == 0 || len(bindings) == 0 {
		return body, nil
	}
	s := string(body)
	for key, val := range bindings {
		ph := "{" + key + "}"
		if !strings.Contains(s, ph) {
			continue
		}
		sv, err := stringifyPathValue(val)
		if err != nil {
			return nil, fmt.Errorf("binding %q: %w", key, err)
		}
		s = strings.ReplaceAll(s, ph, sv)
	}
	return []byte(s), nil
}

func stringifyPathValue(v any) (string, error) {
	switch t := v.(type) {
	case string:
		return t, nil
	case float64, int, int64, uint64, bool:
		return fmt.Sprint(t), nil
	case json.Number:
		return t.String(), nil
	default:
		return "", fmt.Errorf("%w: cannot stringify %T for path/query", ErrBindingTypeMismatch, v)
	}
}

func stringifyHeaderValue(v any) (string, error) { return stringifyPathValue(v) }

// ValidateExpression checks extract expressions at load time.
func ValidateExpression(expr string) error {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return fmt.Errorf("%w: empty expression", ErrConfigError)
	}
	if strings.EqualFold(expr, "status") {
		return nil
	}
	if strings.HasPrefix(strings.ToLower(expr), "header.") {
		if len(expr) <= len("header.") {
			return fmt.Errorf("%w: header expression needs a name", ErrConfigError)
		}
		return nil
	}
	if expr == "$" {
		return nil
	}
	if !strings.HasPrefix(expr, "$") {
		return fmt.Errorf("%w: expression must start with $, status, or header", ErrConfigError)
	}
	if _, err := jsonpath.New(expr); err != nil {
		return fmt.Errorf("%w: invalid JSONPath %q: %v", ErrConfigError, expr, err)
	}
	return nil
}
