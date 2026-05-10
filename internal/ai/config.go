package ai

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const configRelPath = ".config/bt/config.yaml"

type btFileConfig struct {
	AI struct {
		Provider  string `yaml:"provider"`
		APIKey    string `yaml:"api_key"`
		Model     string `yaml:"model"`
		MaxTokens int    `yaml:"max_tokens"`
	} `yaml:"ai"`
}

// LoadProviderConfig reads ANTHROPIC_API_KEY and optional ~/.config/bt/config.yaml.
// Env API key wins over the file. Missing file, unreadable YAML, or no home dir
// are ignored so the CLI still runs with env-only or stub mode.
func LoadProviderConfig() (ProviderConfig, error) {
	cfg := ProviderConfig{
		Model:     defaultModel,
		MaxTokens: 1024,
	}
	if k := os.Getenv("ANTHROPIC_API_KEY"); k != "" {
		cfg.APIKey = k
	}
	if home, err := os.UserHomeDir(); err == nil {
		mergeFileAIConfig(home, &cfg)
	}
	return cfg, nil
}

func mergeFileAIConfig(home string, cfg *ProviderConfig) {
	data, err := os.ReadFile(filepath.Join(home, configRelPath))
	if err != nil {
		return
	}
	var fc btFileConfig
	if err := yaml.Unmarshal(data, &fc); err != nil {
		return
	}
	if cfg.APIKey == "" && fc.AI.APIKey != "" {
		cfg.APIKey = fc.AI.APIKey
	}
	if fc.AI.Model != "" {
		cfg.Model = fc.AI.Model
	}
	if fc.AI.MaxTokens > 0 {
		cfg.MaxTokens = fc.AI.MaxTokens
	}
}
