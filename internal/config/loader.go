package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/viper"
)

var ErrConfigNotFound = errors.New("config file not found")

type Config struct {
	Version    int              `mapstructure:"version"`
	Target     TargetConfig     `mapstructure:"target"`
	Strategies []StrategyConfig `mapstructure:"strategies"`
	Report     ReportConfig     `mapstructure:"report"`
	Safety     SafetyConfig     `mapstructure:"safety"`
}

type TargetConfig struct {
	Name       string     `mapstructure:"name"`
	BaseURL    string     `mapstructure:"base_url"`
	SchemaPath string     `mapstructure:"schema"`
	Auth       AuthConfig `mapstructure:"auth"`
}

type AuthConfig struct {
	Type string `mapstructure:"type"`
	Env  string `mapstructure:"env"`
}

type StrategyConfig struct {
	Type       string         `mapstructure:"type"`
	File       string         `mapstructure:"file"`
	Operations []string       `mapstructure:"operations"`
	Invariants []string       `mapstructure:"invariants"`
	Config     map[string]any `mapstructure:"config"`
}

type ReportConfig struct {
	Formats   []string `mapstructure:"formats"`
	OutputDir string   `mapstructure:"output_dir"`
}

type SafetyConfig struct {
	Profile     string   `mapstructure:"profile"`
	DenyMethods []string `mapstructure:"deny_methods"`
}

func Load(path string) (*Config, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, ErrConfigNotFound
	} else if err != nil {
		return nil, fmt.Errorf("stat config: %w", err)
	}

	v := viper.New()
	v.SetConfigFile(path)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("parse error: %w", err)
	}

	applyDefaults(v)

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func applyDefaults(v *viper.Viper) {
	v.SetDefault("report.formats", []string{"console"})
	v.SetDefault("report.output_dir", ".bt/reports")
	v.SetDefault("safety.profile", "safe")
}

func validate(cfg *Config) error {
	if cfg.Target.Name == "" {
		return errors.New("validation error: target.name is required")
	}
	if cfg.Target.BaseURL == "" {
		return errors.New("validation error: target.base_url is required")
	}
	return nil
}
