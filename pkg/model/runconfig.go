package model

import (
	"encoding/json"
	"strconv"
	"strings"
)

// SafetyConfig is the domain safety configuration carried on a fuzz TestPlan / RunConfig (M5).
type SafetyConfig struct {
	Profile              string   `json:"profile,omitempty"`
	AllowedMethods       []string `json:"allowed_methods,omitempty"`
	DeniedMethods        []string `json:"denied_methods,omitempty"`
	MaxRequestsPerSecond float64  `json:"max_requests_per_second,omitempty"`
	MaxConcurrency       int      `json:"max_concurrency,omitempty"`
	TimeoutSeconds       float64  `json:"timeout_seconds,omitempty"`
}

func (s SafetyConfig) isSet() bool {
	return strings.TrimSpace(s.Profile) != "" ||
		len(s.AllowedMethods) > 0 ||
		len(s.DeniedMethods) > 0 ||
		s.MaxRequestsPerSecond != 0 ||
		s.MaxConcurrency != 0 ||
		s.TimeoutSeconds != 0
}

// RunConfig holds fuzz execution limits and safety (M5).
type RunConfig struct {
	FuzzIterations int          `json:"fuzz_iterations,omitempty"`
	CorpusDir      string       `json:"corpus_dir,omitempty"`
	Safety         SafetyConfig `json:"safety,omitempty"`
	// DestructiveConfirmed is set by the CLI when the user passes --safety destructive; it is not loaded from YAML alone.
	DestructiveConfirmed bool `json:"-"`
}

// TestPlan is a self-contained fuzz plan (M5 runner API); the CLI continues to use strategy.Spec + config files.
type TestPlan struct {
	Target     Target      `json:"target"`
	Operations []Operation `json:"operations"`
	RunConfig  RunConfig   `json:"run_config"`
}

// RunConfigFromMap builds a RunConfig from a strategy spec config map (snake_case keys from YAML).
func RunConfigFromMap(cfg map[string]any) RunConfig {
	if cfg == nil {
		return RunConfig{}
	}
	rc := RunConfig{
		FuzzIterations: intFromAny(cfg["fuzz_iterations"], 0),
		CorpusDir:      stringFromAny(cfg["corpus_dir"]),
	}
	if v, ok := cfg["safety"]; ok {
		rc.Safety = safetyConfigFromAny(v)
	}
	return rc
}

// RunConfigToMap writes rc into a map suitable for strategy.Spec.Config (snake_case).
func RunConfigToMap(rc RunConfig) map[string]any {
	out := map[string]any{}
	if rc.FuzzIterations != 0 {
		out["fuzz_iterations"] = rc.FuzzIterations
	}
	if rc.CorpusDir != "" {
		out["corpus_dir"] = rc.CorpusDir
	}
	if rc.Safety.isSet() {
		out["safety"] = map[string]any{
			"profile":                 rc.Safety.Profile,
			"allowed_methods":         rc.Safety.AllowedMethods,
			"denied_methods":          rc.Safety.DeniedMethods,
			"max_requests_per_second": rc.Safety.MaxRequestsPerSecond,
			"max_concurrency":         rc.Safety.MaxConcurrency,
			"timeout_seconds":         rc.Safety.TimeoutSeconds,
		}
	}
	return out
}

func safetyConfigFromAny(v any) SafetyConfig {
	m, ok := v.(map[string]any)
	if !ok {
		return SafetyConfig{}
	}
	return SafetyConfig{
		Profile:              stringFromAny(m["profile"]),
		AllowedMethods:       stringSliceFromAny(m["allowed_methods"]),
		DeniedMethods:        stringSliceFromAny(m["denied_methods"]),
		MaxRequestsPerSecond: floatFromAny(m["max_requests_per_second"]),
		MaxConcurrency:       intFromAny(m["max_concurrency"], 0),
		TimeoutSeconds:       floatFromAny(m["timeout_seconds"]),
	}
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return ""
	}
}

func intFromAny(v any, def int) int {
	if v == nil {
		return def
	}
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		i, err := t.Int64()
		if err != nil {
			return def
		}
		return int(i)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(t))
		if err != nil {
			return def
		}
		return n
	default:
		return def
	}
}

func floatFromAny(v any) float64 {
	if v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(t), 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

func stringSliceFromAny(v any) []string {
	if v == nil {
		return nil
	}
	switch t := v.(type) {
	case []string:
		return append([]string(nil), t...)
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
