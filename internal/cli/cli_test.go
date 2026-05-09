package cli_test

import (
	"bytes"
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
