package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/viper"

	"github.com/jimbery/bt/pkg/model"
)

var ErrConfigNotFound = errors.New("config file not found")

type Config struct {
	Version    int              `mapstructure:"version"`
	Target     TargetConfig     `mapstructure:"target"`
	Strategies []StrategyConfig `mapstructure:"strategies"`
	Report     ReportConfig     `mapstructure:"report"`
	Safety     SafetyConfig     `mapstructure:"safety"`
	Trace      TraceConfig      `mapstructure:"trace"`
	// Baseline is optional path to baseline YAML (quarantined contract failures). Relative paths resolve from the config file directory.
	Baseline string `mapstructure:"baseline"`
}

type TargetConfig struct {
	Name        string     `mapstructure:"name"`
	BaseURL     string     `mapstructure:"base_url"`
	SchemaPath  string     `mapstructure:"schema"`
	Adapter     string     `mapstructure:"adapter"`
	GraphQLPath string     `mapstructure:"graphql_path"`
	Environment string     `mapstructure:"environment"`
	Auth        AuthConfig `mapstructure:"auth"`
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
	Profile              string   `mapstructure:"profile"`
	DenyMethods          []string `mapstructure:"deny_methods"`
	AllowedMethods       []string `mapstructure:"allowed_methods"`
	MaxRequestsPerSecond float64  `mapstructure:"max_requests_per_second"`
	MaxConcurrency       int      `mapstructure:"max_concurrency"`
	TimeoutSeconds       float64  `mapstructure:"timeout_seconds"`
}

// TraceConfig holds optional trace-profile paths (M12).
type TraceConfig struct {
	// Profile is a path to trace profile JSON (schema_version "1"). Relative paths resolve from the config file directory.
	Profile string `mapstructure:"profile"`
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

var envVarNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func validate(cfg *Config) error {
	if cfg.Target.Name == "" {
		return errors.New("validation error: target.name is required (this file must be a bt config with `target.name`, `target.base_url`, and `target.schema`; a standalone OpenAPI document is not valid here — see testdata/backendtest.yaml)")
	}
	if cfg.Target.BaseURL == "" {
		return errors.New("validation error: target.base_url is required")
	}
	if err := validateBearerAuthEnvName(cfg); err != nil {
		return err
	}
	return nil
}

func validateBearerAuthEnvName(cfg *Config) error {
	t := strings.ToLower(strings.TrimSpace(cfg.Target.Auth.Type))
	if t != "bearer" {
		return nil
	}
	env := strings.TrimSpace(cfg.Target.Auth.Env)
	if env == "" {
		return nil
	}
	if !envVarNamePattern.MatchString(env) {
		return fmt.Errorf(`validation error: target.auth.env must be an environment variable name (e.g. SUBCONTRACTOR_API_TOKEN), not the secret or token literal; set the token in that variable and use "bt run --load-dotenv" or export it before running`)
	}
	return nil
}

// AsModel converts the loaded target configuration into a domain Target.
func (t TargetConfig) AsModel() model.Target {
	adapter := strings.TrimSpace(t.Adapter)
	if adapter == "" {
		adapter = "openapi"
	}
	gqlPath := strings.TrimSpace(t.GraphQLPath)
	if gqlPath == "" {
		gqlPath = "/graphql"
	}
	return model.Target{
		Name:        t.Name,
		BaseURL:     t.BaseURL,
		SchemaPath:  t.SchemaPath,
		Adapter:     adapter,
		GraphQLPath: gqlPath,
		Environment: t.Environment,
		Auth: model.AuthConfig{
			Type: t.Auth.Type,
			Env:  t.Auth.Env,
		},
	}
}
