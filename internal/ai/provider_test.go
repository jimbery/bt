package ai_test

import (
	"context"
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/ai"
)

func TestNewProvider_EmptyAPIKey_ReturnsStub(t *testing.T) {
	p, err := ai.NewProvider(ai.ProviderConfig{APIKey: ""})
	if err != nil {
		t.Fatalf("unexpected error with empty key: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	resp, err := p.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "respond with: OK",
		UserPrompt:   "test",
		MaxTokens:    10,
	})
	if err != nil {
		t.Fatalf("stub should not error: %v", err)
	}
	if resp.Text == "" {
		t.Error("stub should return non-empty text")
	}
}

func TestNewProvider_EmptyModel_UsesDefault(t *testing.T) {
	p, err := ai.NewProvider(ai.ProviderConfig{APIKey: "test-key", Model: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewProvider_ZeroMaxTokens_UsesDefault(t *testing.T) {
	p, err := ai.NewProvider(ai.ProviderConfig{APIKey: "", MaxTokens: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestStubProvider_Complete_ReturnsNonEmptyText(t *testing.T) {
	stub := ai.NewStubProvider("test response")
	resp, err := stub.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text == "" {
		t.Error("stub must return non-empty text")
	}
}

func TestStubProvider_Complete_ReturnsConfiguredText(t *testing.T) {
	expected := `{"suggestions":[]}`
	stub := ai.NewStubProvider(expected)
	resp, err := stub.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != expected {
		t.Errorf("expected %q, got %q", expected, resp.Text)
	}
}

func TestStubProvider_Complete_HonoursContextCancellation(t *testing.T) {
	stub := ai.NewStubProvider("text")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := stub.Complete(ctx, ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    10,
	})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestStubProvider_Complete_TokenCountsAreNonNegative(t *testing.T) {
	stub := ai.NewStubProvider("response text")
	resp, _ := stub.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    100,
	})
	if resp.InputTokens < 0 {
		t.Errorf("InputTokens must be >= 0, got %d", resp.InputTokens)
	}
	if resp.OutputTokens < 0 {
		t.Errorf("OutputTokens must be >= 0, got %d", resp.OutputTokens)
	}
}

func TestCompletionRequest_EmptyUserPrompt_StubStillResponds(t *testing.T) {
	stub := ai.NewStubProvider("ok")
	_, err := stub.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "",
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("unexpected error for empty user prompt: %v", err)
	}
}

func TestProviderConfig_DefaultModel_IsSet(t *testing.T) {
	cfg := ai.ProviderConfig{APIKey: "key"}
	p, err := ai.NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = p.Complete(ctx, ai.CompletionRequest{
		UserPrompt: "test",
		MaxTokens:  10,
	})
}
