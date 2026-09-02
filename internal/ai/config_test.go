package ai_test

import (
	"testing"

	"github.com/jimbery/bt/internal/ai"
)

func TestLoadProviderConfig_EnvVarTakesPrecedence(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "env-key")
	cfg, err := ai.LoadProviderConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "env-key" {
		t.Errorf("expected API key from env var, got %q", cfg.APIKey)
	}
}

func TestLoadProviderConfig_NoKeySet_ReturnsEmptyKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	cfg, err := ai.LoadProviderConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "" {
		t.Errorf("expected empty API key when none configured, got %q", cfg.APIKey)
	}
}

func TestLoadProviderConfig_EmptyKey_DefaultModelIsSet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	cfg, err := ai.LoadProviderConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Model == "" {
		t.Error("default model must be set even when no key is configured")
	}
}

func TestLoadProviderConfig_DefaultModel_IsClaude(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	cfg, err := ai.LoadProviderConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Model != "claude-sonnet-4-5" {
		t.Errorf("expected default model 'claude-sonnet-4-5', got %q", cfg.Model)
	}
}
