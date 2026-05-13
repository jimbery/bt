package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Trace profile persistence (ADR-008 style).

var (
	ErrProfileNotFound        = errors.New("trace profile: file not found")
	ErrProfileMalformed       = errors.New("trace profile: malformed")
	ErrProfileVersionMismatch = errors.New("trace profile: unsupported schema_version")
)

func IsErrProfileNotFound(err error) bool        { return errors.Is(err, ErrProfileNotFound) }
func IsErrProfileMalformed(err error) bool       { return errors.Is(err, ErrProfileMalformed) }
func IsErrProfileVersionMismatch(err error) bool { return errors.Is(err, ErrProfileVersionMismatch) }

const traceProfileSchemaVersion = "1"

const distributionSumTolerance = 0.001

// TraceProfile is the JSON-serialisable output of HAR analysis (M12).
type TraceProfile struct {
	SchemaVersion string                       `json:"schema_version"`
	GeneratedAt   string                       `json:"generated_at"`
	SourceHAR     string                       `json:"source_har,omitempty"`
	Operations    map[string]*OperationProfile `json:"operations"`
	Sequences     *SequenceProfile             `json:"sequences,omitempty"`
}

// OperationProfile holds per-operation statistics from trace analysis.
type OperationProfile struct {
	CallCount     int                         `json:"call_count"`
	FrequencyRank int                         `json:"frequency_rank"`
	Arguments     map[string]*ArgumentProfile `json:"arguments"`
}

// ArgumentProfile describes how a single request field was observed in traces.
type ArgumentProfile struct {
	Type          string             `json:"type"`
	Samples       []any              `json:"samples,omitempty"`
	Distribution  map[string]float64 `json:"distribution,omitempty"`
	Range         *Range             `json:"range,omitempty"`
	NullRate      float64            `json:"null_rate"`
	AlwaysPresent bool               `json:"always_present"`
}

// Range is a numeric min/max window from observed values.
type Range struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// SequenceProfile holds Markov-style operation transition data (M12; consumed further in M13).
type SequenceProfile struct {
	StartProbability         map[string]float64            `json:"start_probability"`
	Transitions              map[string]map[string]float64 `json:"transitions"`
	MinObservedSessionLength int                           `json:"min_observed_session_length"`
	MaxObservedSessionLength int                           `json:"max_observed_session_length"`
}

// ParseProfile reads and validates a TraceProfile JSON file from disk.
func ParseProfile(path string) (*TraceProfile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrProfileNotFound, path)
		}
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProfileMalformed, err)
	}
	ver, ok := raw["schema_version"]
	if !ok {
		return nil, fmt.Errorf("%w: missing schema_version", ErrProfileMalformed)
	}
	var schemaVersion string
	if err := json.Unmarshal(ver, &schemaVersion); err != nil {
		return nil, fmt.Errorf("%w: schema_version: %v", ErrProfileMalformed, err)
	}
	if schemaVersion != traceProfileSchemaVersion {
		return nil, fmt.Errorf("%w: got %q (upgrade bt to a version that supports this profile format)", ErrProfileVersionMismatch, schemaVersion)
	}
	if _, ok := raw["operations"]; !ok {
		return nil, fmt.Errorf("%w: missing operations", ErrProfileMalformed)
	}

	var p TraceProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProfileMalformed, err)
	}
	if p.Operations == nil {
		return nil, fmt.Errorf("%w: operations is null", ErrProfileMalformed)
	}
	for opID, op := range p.Operations {
		if op == nil {
			return nil, fmt.Errorf("%w: operation %q is null", ErrProfileMalformed, opID)
		}
		for argName, ap := range op.Arguments {
			if ap == nil {
				return nil, fmt.Errorf("%w: operation %q argument %q is null", ErrProfileMalformed, opID, argName)
			}
			if len(ap.Distribution) > 0 {
				sum := 0.0
				for _, v := range ap.Distribution {
					sum += v
				}
				if sum < 1.0-distributionSumTolerance || sum > 1.0+distributionSumTolerance {
					return nil, fmt.Errorf("%w: operation %q argument %q distribution sums to %f", ErrProfileMalformed, opID, argName, sum)
				}
			}
		}
	}
	if p.Sequences != nil {
		if err := validateSequenceProfile(p.Sequences); err != nil {
			return nil, err
		}
	}
	if _, err := time.Parse(time.RFC3339, p.GeneratedAt); err != nil && strings.TrimSpace(p.GeneratedAt) != "" {
		// Allow empty GeneratedAt for hand-written tests; Analyze always sets RFC3339.
		if p.GeneratedAt != "" {
			return nil, fmt.Errorf("%w: generated_at: %v", ErrProfileMalformed, err)
		}
	}
	return &p, nil
}

func validateSequenceProfile(s *SequenceProfile) error {
	if s == nil {
		return nil
	}
	sum := 0.0
	for _, p := range s.StartProbability {
		sum += p
	}
	if len(s.StartProbability) > 0 && (sum < 1.0-distributionSumTolerance || sum > 1.0+distributionSumTolerance) {
		return fmt.Errorf("%w: start_probability sums to %f", ErrProfileMalformed, sum)
	}
	for from, row := range s.Transitions {
		t := 0.0
		for _, p := range row {
			t += p
		}
		if len(row) > 0 && (t < 1.0-distributionSumTolerance || t > 1.0+distributionSumTolerance) {
			return fmt.Errorf("%w: transitions[%q] sums to %f", ErrProfileMalformed, from, t)
		}
	}
	return nil
}

// WriteToFile serialises the profile to JSON and writes it to path (mkdir parents).
func (p *TraceProfile) WriteToFile(path string) error {
	if p == nil {
		return fmt.Errorf("%w: nil profile", ErrProfileMalformed)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}
