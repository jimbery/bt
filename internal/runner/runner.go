package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jimbery/bt/pkg/model"
)

// Config holds runner configuration.
type Config struct {
	BaseURL string
	Timeout time.Duration
}

// Runner executes CaseInputs against a real HTTP endpoint.
type Runner struct {
	client  *http.Client
	baseURL string
}

// DefaultTimeout is used when runner.Config.Timeout is zero.
const DefaultTimeout = 30 * time.Second

// New returns a new Runner with the given config.
func New(cfg Config) *Runner {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	return &Runner{
		client:  &http.Client{Timeout: timeout},
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
	}
}

// Run executes a single CaseInput and returns the ResponseDetail.
// Non-2xx responses are not treated as errors — invariant evaluation
// decides whether a given status code is a failure.
func (r *Runner) Run(ctx context.Context, input model.CaseInput) (model.ResponseDetail, error) {
	base, err := url.Parse(r.baseURL)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("invalid base URL: %w", err)
	}
	rel, err := url.Parse(input.Path)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("invalid path: %w", err)
	}
	u := base.ResolveReference(rel)

	if len(input.Query) > 0 {
		q := u.Query()
		for k, v := range input.Query {
			q.Set(k, v)
		}
		u.RawQuery = q.Encode()
	}

	var bodyReader io.Reader
	if input.Body != nil {
		switch b := input.Body.(type) {
		case json.RawMessage:
			if len(b) > 0 {
				bodyReader = bytes.NewReader([]byte(b))
			}
		default:
			data, err := json.Marshal(input.Body)
			if err != nil {
				return model.ResponseDetail{}, fmt.Errorf("cannot marshal body: %w", err)
			}
			bodyReader = bytes.NewReader(data)
		}
	}

	req, err := http.NewRequestWithContext(ctx, input.Method, u.String(), bodyReader)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("cannot build request: %w", err)
	}

	if input.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range input.Headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("cannot read response body: %w", err)
	}

	headers := make(map[string]string, len(resp.Header))
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return model.ResponseDetail{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       body,
	}, nil
}
