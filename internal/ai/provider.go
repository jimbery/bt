package ai

import (
	"context"
	"fmt"
)

const defaultModel = "claude-sonnet-4-5"

// Provider sends prompts to a completion backend.
type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// CompletionRequest is a single prompt exchange.
type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	MaxTokens    int
}

// CompletionResponse holds raw model text and usage estimates.
type CompletionResponse struct {
	Text         string
	InputTokens  int
	OutputTokens int
}

// ProviderConfig selects the backend and limits.
type ProviderConfig struct {
	APIKey    string
	Model     string
	MaxTokens int
}

// NewProvider returns a StubProvider when APIKey is empty, otherwise AnthropicProvider.
func NewProvider(cfg ProviderConfig) (Provider, error) {
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1024
	}
	if cfg.Model == "" {
		cfg.Model = defaultModel
	}
	if cfg.APIKey == "" {
		return newStubProvider(""), nil
	}
	return newAnthropicProvider(cfg.APIKey, cfg.Model, cfg.MaxTokens), nil
}

// NewStubProvider returns a Provider that echoes fixed text without network I/O.
// If text is empty, a minimal JSON array is returned so callers always get parseable output.
func NewStubProvider(text string) Provider {
	return newStubProvider(text)
}

func newStubProvider(text string) Provider {
	if text == "" {
		text = "[]"
	}
	return &stubProvider{text: text}
}

// ProviderLabel reports whether completions use the stub or Anthropic backend.
func ProviderLabel(p Provider) string {
	switch p.(type) {
	case *stubProvider:
		return "stub"
	case *anthropicProvider:
		return "ai"
	default:
		return "unknown"
	}
}

func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	// Rough heuristic: ~4 chars per token.
	return (len(s) + 3) / 4
}

func clampTokens(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// ErrAnthropicHTTP is returned for non-success Anthropic API responses.
type ErrAnthropicHTTP struct {
	StatusCode int
	Body       string
}

func (e ErrAnthropicHTTP) Error() string {
	return fmt.Sprintf("anthropic api: status %d: %s", e.StatusCode, e.Body)
}
