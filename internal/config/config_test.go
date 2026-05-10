package config_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jayimbery/bt/internal/config"
)

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()
	_, err := config.Load("/nonexistent/path/backendtest.yaml")
	if !errors.Is(err, config.ErrConfigNotFound) {
		t.Errorf("expected ErrConfigNotFound, got %v", err)
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(path, []byte(":::invalid yaml:::"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if errors.Is(err, config.ErrConfigNotFound) {
		t.Error("should not return ErrConfigNotFound for invalid YAML")
	}
}

func TestLoad_MissingRequiredField_TargetName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	yaml := "version: 1\ntarget:\n  base_url: https://staging.example.com\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected validation error for missing target.name")
	}
}

func TestLoad_MissingRequiredField_TargetBaseURL(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	yaml := "version: 1\ntarget:\n  name: orders-api\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected validation error for missing target.base_url")
	}
}

func TestLoad_ValidMinimalConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	yaml := "version: 1\ntarget:\n  name: orders-api\n  base_url: https://staging.example.com\n  schema: ./openapi.yaml\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Target.Name != "orders-api" {
		t.Errorf("Target.Name: got %q, want %q", cfg.Target.Name, "orders-api")
	}
	if cfg.Target.BaseURL != "https://staging.example.com" {
		t.Errorf("Target.BaseURL: got %q, want %q", cfg.Target.BaseURL, "https://staging.example.com")
	}
}

func TestLoad_DefaultsApplied(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	yaml := "version: 1\ntarget:\n  name: orders-api\n  base_url: https://staging.example.com\n  schema: ./openapi.yaml\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Report.Formats) == 0 {
		t.Error("expected default report formats to be applied")
	}
	if cfg.Report.Formats[0] != "console" {
		t.Errorf("default report format: got %q, want %q", cfg.Report.Formats[0], "console")
	}
	if cfg.Safety.Profile != "safe" {
		t.Errorf("default safety profile: got %q, want %q", cfg.Safety.Profile, "safe")
	}
}

func TestLoad_FullConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	yaml := `version: 1
target:
  name: orders-api
  base_url: https://staging.example.com
  schema: ./openapi.yaml
  auth:
    type: bearer
    env: ORDERS_API_TOKEN
strategies:
  - type: table
    file: ./tests/orders-table.yaml
  - type: property
    operations: [CreateOrder, GetOrder]
    invariants:
      - no_5xx
      - response_matches_schema
    config:
      max_examples: 100
      seed: 12345
report:
  formats: [console, junit, json]
  output_dir: ./.bt/reports
safety:
  profile: non_destructive
  deny_methods: [DELETE]
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("unexpected error loading full config: %v", err)
	}
	if len(cfg.Strategies) != 2 {
		t.Errorf("Strategies length: got %d, want 2", len(cfg.Strategies))
	}
	if cfg.Strategies[1].Type != "property" {
		t.Errorf("Strategies[1].Type: got %q, want %q", cfg.Strategies[1].Type, "property")
	}
	if cfg.Safety.Profile != "non_destructive" {
		t.Errorf("Safety.Profile: got %q, want %q", cfg.Safety.Profile, "non_destructive")
	}
	if len(cfg.Safety.DenyMethods) != 1 || cfg.Safety.DenyMethods[0] != "DELETE" {
		t.Errorf("Safety.DenyMethods: got %v, want [DELETE]", cfg.Safety.DenyMethods)
	}
}

func TestLoad_InvalidBearerAuthEnvNotEnvVarName(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	yaml := `version: 1
target:
  name: orders-api
  base_url: https://staging.example.com
  schema: ./openapi.yaml
  auth:
    type: bearer
    env: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.e30
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected validation error for bearer auth env that is not an env var name")
	}
}

func TestScaffold_CreatesParentDirs(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	path := filepath.Join(base, "nested", "dir", "backendtest.yaml")
	if err := config.Scaffold(path, false); err != nil {
		t.Fatalf("Scaffold returned unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected file at %s: %v", path, err)
	}
	if _, err := config.Load(path); err != nil {
		t.Fatalf("scaffolded config failed to load: %v", err)
	}
}

func TestScaffold_WritesValidFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")

	if err := config.Scaffold(path, false); err != nil {
		t.Fatalf("Scaffold returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read scaffolded file: %v", err)
	}
	if len(data) == 0 {
		t.Error("scaffolded file must not be empty")
	}

	if _, err := config.Load(path); err != nil {
		t.Errorf("scaffolded config failed to load: %v", err)
	}
}

func TestScaffold_RefusesOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Scaffold(path, false); err == nil {
		t.Error("expected error when overwriting existing file without force flag")
	}
}

func TestScaffold_ForceOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.Scaffold(path, true); err != nil {
		t.Errorf("unexpected error with force flag: %v", err)
	}
}
