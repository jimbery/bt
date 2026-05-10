package bt_test

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type fuzzConfig struct {
	Version    int    `yaml:"version"`
	Target     target `yaml:"target"`
	Strategies []struct {
		Type       string         `yaml:"type"`
		Config     map[string]any `yaml:"config"`
		Operations []string       `yaml:"operations"`
	} `yaml:"strategies"`
}

func TestFuzzConfig_IsValidYAML(t *testing.T) {
	data, err := os.ReadFile("backendtest-fuzz.yaml")
	if err != nil {
		t.Fatalf("cannot read backendtest-fuzz.yaml: %v", err)
	}
	var cfg fuzzConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("backendtest-fuzz.yaml is not valid YAML: %v", err)
	}
}

func TestFuzzConfig_StrategyType_IsFuzz(t *testing.T) {
	data, _ := os.ReadFile("backendtest-fuzz.yaml")
	var cfg fuzzConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Strategies) == 0 {
		t.Fatal("expected at least one strategy")
	}
	if cfg.Strategies[0].Type != "fuzz" {
		t.Errorf("expected strategy type 'fuzz', got %q", cfg.Strategies[0].Type)
	}
}

func TestFuzzConfig_SafetyProfile_IsSafe(t *testing.T) {
	data, _ := os.ReadFile("backendtest-fuzz.yaml")
	raw := make(map[string]any)
	if err := yaml.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	safety, _ := raw["safety"].(map[string]any)
	if safety == nil {
		t.Fatal("expected safety block")
	}
	if safety["profile"] != "safe" {
		t.Errorf("expected safety profile 'safe', got %v", safety["profile"])
	}
}

func TestFuzzConfig_NoDeleteOperations(t *testing.T) {
	data, _ := os.ReadFile("backendtest-fuzz.yaml")
	if strings.Contains(string(data), "DeleteOrder") {
		t.Error("DeleteOrder must not appear in the fuzz config operations list")
	}
}

func TestFuzzConfig_Iterations_IsPositive(t *testing.T) {
	data, _ := os.ReadFile("backendtest-fuzz.yaml")
	var cfg fuzzConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	raw := cfg.Strategies[0].Config["fuzz_iterations"]
	var n int
	switch v := raw.(type) {
	case int:
		n = v
	case int64:
		n = int(v)
	case float64:
		n = int(v)
	default:
		t.Fatalf("unexpected fuzz_iterations type %T", raw)
	}
	if n <= 0 {
		t.Fatalf("expected fuzz_iterations > 0, got %d", n)
	}
}

func TestFuzzConfig_CorpusDir_IsSet(t *testing.T) {
	data, _ := os.ReadFile("backendtest-fuzz.yaml")
	var cfg fuzzConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	dir, _ := cfg.Strategies[0].Config["corpus_dir"].(string)
	if dir == "" {
		t.Error("expected corpus_dir to be set in fuzz strategy config")
	}
}

func TestFuzzConfig_OperationsIncludeCreateAndBroken(t *testing.T) {
	data, _ := os.ReadFile("backendtest-fuzz.yaml")
	var cfg fuzzConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{"CreateOrder": false, "GetOrderBroken": false}
	for _, op := range cfg.Strategies[0].Operations {
		if _, ok := required[op]; ok {
			required[op] = true
		}
	}
	for name, found := range required {
		if !found {
			t.Errorf("expected operation %q in fuzz config", name)
		}
	}
}
