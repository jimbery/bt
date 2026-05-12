package runplan

import (
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/runner"
)

func TestRequestHTTPTimeout_nilConfig(t *testing.T) {
	t.Parallel()
	if got := requestHTTPTimeout(nil); got != runner.DefaultTimeout {
		t.Fatalf("got %v, want %v", got, runner.DefaultTimeout)
	}
}

func TestRequestHTTPTimeout_zeroUsesDefault(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Safety: config.SafetyConfig{TimeoutSeconds: 0}}
	if got := requestHTTPTimeout(cfg); got != runner.DefaultTimeout {
		t.Fatalf("got %v, want %v", got, runner.DefaultTimeout)
	}
}

func TestRequestHTTPTimeout_negativeUsesDefault(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Safety: config.SafetyConfig{TimeoutSeconds: -1}}
	if got := requestHTTPTimeout(cfg); got != runner.DefaultTimeout {
		t.Fatalf("got %v, want %v", got, runner.DefaultTimeout)
	}
}

func TestRequestHTTPTimeout_fromSafetySeconds(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{Safety: config.SafetyConfig{TimeoutSeconds: 90}}
	if got := requestHTTPTimeout(cfg); got != 90*time.Second {
		t.Fatalf("got %v, want 90s", got)
	}
}
