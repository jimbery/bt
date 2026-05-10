package config

import (
	"testing"
)

func TestRequestHeaderOverrides_bearerRawToken(t *testing.T) {
	t.Setenv("MY_TOKEN", "secret")
	got := RequestHeaderOverrides(AuthConfig{Type: "bearer", Env: "MY_TOKEN"})
	if got == nil || got["Authorization"] != "Bearer secret" {
		t.Fatalf("got %#v", got)
	}
}

func TestRequestHeaderOverrides_bearerPrefixed(t *testing.T) {
	t.Setenv("MY_TOKEN", "Bearer already")
	got := RequestHeaderOverrides(AuthConfig{Type: "bearer", Env: "MY_TOKEN"})
	if got == nil || got["Authorization"] != "Bearer already" {
		t.Fatalf("got %#v", got)
	}
}

func TestRequestHeaderOverrides_emptyEnv(t *testing.T) {
	t.Setenv("MY_TOKEN", "")
	got := RequestHeaderOverrides(AuthConfig{Type: "bearer", Env: "MY_TOKEN"})
	if got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}

func TestRequestHeaderOverrides_unknownType(t *testing.T) {
	t.Setenv("X", "y")
	got := RequestHeaderOverrides(AuthConfig{Type: "hmac", Env: "X"})
	if got != nil {
		t.Fatalf("expected nil, got %#v", got)
	}
}
