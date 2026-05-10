package bt_test

import (
	"os"
	"testing"

	"gopkg.in/yaml.v3"
)

type propertyConfig struct {
	Version    int    `yaml:"version"`
	Target     target `yaml:"target"`
	Strategies []struct {
		Type       string         `yaml:"type"`
		Config     map[string]any `yaml:"config"`
		Invariants []string       `yaml:"invariants"`
		Operations []string       `yaml:"operations"`
	} `yaml:"strategies"`
}

type target struct {
	Name    string `yaml:"name"`
	BaseURL string `yaml:"base_url"`
	Schema  string `yaml:"schema"`
}

func TestPropertyConfig_IsValidYAML(t *testing.T) {
	data, err := os.ReadFile("backendtest-property.yaml")
	if err != nil {
		t.Fatalf("cannot read backendtest-property.yaml: %v", err)
	}

	var cfg propertyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("backendtest-property.yaml is not valid YAML: %v", err)
	}
}

func TestPropertyConfig_Version_IsOne(t *testing.T) {
	data, err := os.ReadFile("backendtest-property.yaml")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var cfg propertyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if cfg.Version != 1 {
		t.Errorf("expected version 1, got %d", cfg.Version)
	}
}

func TestPropertyConfig_Strategy_IsProperty(t *testing.T) {
	data, err := os.ReadFile("backendtest-property.yaml")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var cfg propertyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	if len(cfg.Strategies) == 0 {
		t.Fatal("expected at least one strategy")
	}
	if cfg.Strategies[0].Type != "property" {
		t.Errorf("expected strategy type 'property', got %q", cfg.Strategies[0].Type)
	}
}

func TestPropertyConfig_Invariants_IncludeRequiredSet(t *testing.T) {
	data, err := os.ReadFile("backendtest-property.yaml")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var cfg propertyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	required := map[string]bool{
		"no_5xx":                  false,
		"response_matches_schema": false,
	}
	for _, inv := range cfg.Strategies[0].Invariants {
		if _, ok := required[inv]; ok {
			required[inv] = true
		}
	}
	for name, found := range required {
		if !found {
			t.Errorf("required invariant %q not found in config", name)
		}
	}
}

func TestPropertyConfig_Operations_IncludeBrokenEndpoint(t *testing.T) {
	data, err := os.ReadFile("backendtest-property.yaml")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var cfg propertyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	found := false
	for _, op := range cfg.Strategies[0].Operations {
		if op == "GetOrderBroken" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected GetOrderBroken in property test operations")
	}
}

func TestPropertyConfig_Operations_IncludeCreateOrder(t *testing.T) {
	data, err := os.ReadFile("backendtest-property.yaml")
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	var cfg propertyConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	found := false
	for _, op := range cfg.Strategies[0].Operations {
		if op == "CreateOrder" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected CreateOrder in property test operations")
	}
}
