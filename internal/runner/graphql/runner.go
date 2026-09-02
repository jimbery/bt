package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jimbery/bt/pkg/model"
)

// ErrNotGraphQL is returned when Run is called with a non-GraphQL CaseInput.
var ErrNotGraphQL = errors.New("graphql runner: input is not a GraphQL case (GQLQuery is empty)")

// Config configures the GraphQL runner.
type Config struct {
	BaseURL string
	Timeout time.Duration
}

// Runner executes GraphQL operations over HTTP.
type Runner struct {
	client  *http.Client
	baseURL string
}

// New returns a Runner with sensible defaults.
func New(cfg Config) *Runner {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Runner{
		client:  &http.Client{Timeout: timeout},
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
	}
}

// Run executes a GraphQL operation. Returns ErrNotGraphQL if input.IsGraphQL() is false.
func (r *Runner) Run(ctx context.Context, input model.CaseInput) (model.ResponseDetail, error) {
	if !input.IsGraphQL() {
		return model.ResponseDetail{}, ErrNotGraphQL
	}

	payload := map[string]any{
		"query": input.GQLQuery,
	}
	if strings.TrimSpace(input.GQLOperationName) != "" {
		payload["operationName"] = input.GQLOperationName
	}
	if input.GQLVariables != nil {
		payload["variables"] = input.GQLVariables
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("marshal request: %w", err)
	}

	path := strings.TrimSpace(input.Path)
	if path == "" {
		path = "/graphql"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := r.baseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range input.Headers {
		req.Header.Set(k, v)
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("execute request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.ResponseDetail{}, fmt.Errorf("read response body: %w", err)
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
		Body:       respBody,
	}, nil
}
