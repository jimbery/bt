package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jayimbery/bt/internal/cli"
)

func TestInitCommand_CreatesConfigFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "backendtest.yaml")

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"init", "--config", configPath})
	cmd.SetOut(&bytes.Buffer{})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("init command failed: %v", err)
	}
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("expected config file to be created")
	}
}

func TestInitCommand_RefusesOverwriteWithoutForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(configPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"init", "--config", configPath})
	if err := cmd.Execute(); err == nil {
		t.Error("expected error when config already exists without --force")
	}
}

func TestInitCommand_ForceOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(configPath, []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"init", "--config", configPath, "--force"})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Errorf("expected init --force to succeed, got: %v", err)
	}
}

func TestValidateCommand_ValidConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "backendtest.yaml")
	yaml := "version: 1\ntarget:\n  name: orders-api\n  base_url: https://staging.example.com\n  schema: ./openapi.yaml\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"validate", "--config", configPath})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err != nil {
		t.Errorf("expected validate to succeed, got: %v", err)
	}
}

func TestValidateCommand_InvalidConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "backendtest.yaml")
	yaml := "version: 1\ntarget:\n  base_url: https://staging.example.com\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"validate", "--config", configPath})
	if err := cmd.Execute(); err == nil {
		t.Error("expected validate to fail for invalid config")
	}
}

func TestValidateCommand_MissingFile(t *testing.T) {
	t.Parallel()
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"validate", "--config", "/nonexistent/backendtest.yaml"})
	if err := cmd.Execute(); err == nil {
		t.Error("expected validate to fail for missing config file")
	}
}

func TestValidateCommand_JSONOutput_ValidConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "backendtest.yaml")
	yaml := "version: 1\ntarget:\n  name: orders-api\n  base_url: https://staging.example.com\n  schema: ./openapi.yaml\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"validate", "--config", configPath, "--output", "json"})
	cmd.SetOut(buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, buf.String())
	}
	if v, ok := got["valid"].(bool); !ok || !v {
		t.Fatalf("expected valid=true, got %v", got)
	}
}

func TestValidateCommand_JSONOutput_InvalidConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "backendtest.yaml")
	yaml := "version: 1\ntarget:\n  base_url: https://staging.example.com\n"
	if err := os.WriteFile(configPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	buf := &bytes.Buffer{}
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"validate", "--config", configPath, "--output", "json"})
	cmd.SetOut(buf)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected validate to fail for invalid config")
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, buf.String())
	}
	if v, ok := got["valid"].(bool); !ok || v {
		t.Fatalf("expected valid=false, got %v", got)
	}
}

func TestValidateCommand_JSONOutput_MissingFile(t *testing.T) {
	t.Parallel()
	buf := &bytes.Buffer{}
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"validate", "--config", "/nonexistent/backendtest.yaml", "--output", "json"})
	cmd.SetOut(buf)
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, buf.String())
	}
	if v, ok := got["valid"].(bool); !ok || v {
		t.Fatalf("expected valid=false, got %v", got)
	}
	errs, ok := got["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("expected errors array, got %v", got)
	}
}
