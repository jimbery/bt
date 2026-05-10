package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type anthropicProvider struct {
	apiKey    string
	model     string
	maxTokens int
	client    *http.Client
}

func newAnthropicProvider(apiKey, model string, maxTokens int) *anthropicProvider {
	return &anthropicProvider{
		apiKey:    apiKey,
		model:     model,
		maxTokens: maxTokens,
		client:    &http.Client{Timeout: 5 * time.Minute},
	}
}

type anthropicRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	System    string         `json:"system"`
	Messages  []anthropicMsg `json:"messages"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

const anthropicOverloaded = 529

func (a *anthropicProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	if err := ctx.Err(); err != nil {
		return CompletionResponse{}, err
	}
	maxTok := req.MaxTokens
	if maxTok <= 0 {
		maxTok = a.maxTokens
	}
	body, err := json.Marshal(anthropicRequest{
		Model:     a.model,
		MaxTokens: maxTok,
		System:    req.SystemPrompt,
		Messages:  []anthropicMsg{{Role: "user", Content: req.UserPrompt}},
	})
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("marshal anthropic request: %w", err)
	}

	do := func() (*http.Response, []byte, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
		if err != nil {
			return nil, nil, err
		}
		r.Header.Set("x-api-key", a.apiKey)
		r.Header.Set("anthropic-version", "2023-06-01")
		r.Header.Set("content-type", "application/json")
		resp, err := a.client.Do(r)
		if err != nil {
			return nil, nil, err
		}
		b, readErr := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if readErr != nil {
			return nil, nil, readErr
		}
		return resp, b, nil
	}

	resp, b, err := do()
	if err != nil {
		return CompletionResponse{}, err
	}

	if resp.StatusCode == anthropicOverloaded {
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return CompletionResponse{}, ctx.Err()
		}
		resp, b, err = do()
		if err != nil {
			return CompletionResponse{}, err
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return CompletionResponse{}, ErrAnthropicHTTP{StatusCode: resp.StatusCode, Body: string(b)}
	}

	var out anthropicResponse
	if err := json.Unmarshal(b, &out); err != nil {
		return CompletionResponse{}, fmt.Errorf("decode anthropic response: %w", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return CompletionResponse{}, fmt.Errorf("anthropic error: %s", out.Error.Message)
	}
	text := ""
	for _, c := range out.Content {
		if c.Type == "text" {
			text += c.Text
		}
	}
	return CompletionResponse{
		Text:         text,
		InputTokens:  clampTokens(out.Usage.InputTokens),
		OutputTokens: clampTokens(out.Usage.OutputTokens),
	}, nil
}
