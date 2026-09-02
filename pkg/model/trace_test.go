package model_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jimbery/bt/pkg/model"
)

func writeTempProfile(t *testing.T, content any) string {
	t.Helper()
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func validProfileData() map[string]any {
	return map[string]any{
		"schema_version": "1",
		"generated_at":   "2024-01-15T10:00:00Z",
		"source_har":     "sample.har",
		"operations": map[string]any{
			"CreateOrder": map[string]any{
				"call_count":     30,
				"frequency_rank": 1,
				"arguments": map[string]any{
					"currency": map[string]any{
						"type":           "string",
						"samples":        []string{"GBP", "USD", "EUR"},
						"distribution":   map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05},
						"null_rate":      0.0,
						"always_present": true,
					},
				},
			},
		},
		"sequences": map[string]any{
			"start_probability":           map[string]float64{"CreateOrder": 1.0},
			"transitions":                 map[string]any{"CreateOrder": map[string]float64{"__END__": 1.0}},
			"min_observed_session_length": 1,
			"max_observed_session_length": 1,
		},
	}
}

func TestParseProfile_ValidFile_ParsesCorrectly(t *testing.T) {
	path := writeTempProfile(t, validProfileData())
	profile, err := model.ParseProfile(path)
	if err != nil {
		t.Fatalf("ParseProfile: %v", err)
	}

	t.Run("schema version is '1'", func(t *testing.T) {
		if profile.SchemaVersion != "1" {
			t.Errorf("want '1', got %q", profile.SchemaVersion)
		}
	})
	t.Run("generated_at is RFC3339 parseable", func(t *testing.T) {
		if _, err := time.Parse(time.RFC3339, profile.GeneratedAt); err != nil {
			t.Errorf("GeneratedAt not RFC3339: %v", err)
		}
	})
	t.Run("CreateOrder operation is present", func(t *testing.T) {
		if _, ok := profile.Operations["CreateOrder"]; !ok {
			t.Error("expected CreateOrder in Operations")
		}
	})
	t.Run("currency argument distribution is populated", func(t *testing.T) {
		arg := profile.Operations["CreateOrder"].Arguments["currency"]
		if len(arg.Distribution) == 0 {
			t.Error("expected non-empty distribution for currency")
		}
		if arg.Distribution["GBP"] != 0.70 {
			t.Errorf("GBP distribution: want 0.70, got %f", arg.Distribution["GBP"])
		}
	})
	t.Run("always_present is true for currency", func(t *testing.T) {
		arg := profile.Operations["CreateOrder"].Arguments["currency"]
		if !arg.AlwaysPresent {
			t.Error("expected AlwaysPresent=true for currency")
		}
	})
	t.Run("sequences are present", func(t *testing.T) {
		if profile.Sequences == nil {
			t.Error("expected non-nil Sequences")
		}
		if profile.Sequences.StartProbability["CreateOrder"] != 1.0 {
			t.Errorf("StartProbability[CreateOrder]: want 1.0, got %f",
				profile.Sequences.StartProbability["CreateOrder"])
		}
	})
}

func TestParseProfile_MissingFile_ReturnsErrProfileNotFound(t *testing.T) {
	_, err := model.ParseProfile("/nonexistent/path/profile.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !model.IsErrProfileNotFound(err) {
		t.Errorf("expected ErrProfileNotFound, got: %T: %v", err, err)
	}
}

func TestParseProfile_InvalidJSON_ReturnsErrProfileMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	_ = os.WriteFile(path, []byte("not json"), 0o644)

	_, err := model.ParseProfile(path)
	if !model.IsErrProfileMalformed(err) {
		t.Errorf("expected ErrProfileMalformed, got: %T: %v", err, err)
	}
}

func TestParseProfile_UnknownSchemaVersion_ReturnsErrProfileVersionMismatch(t *testing.T) {
	data := validProfileData()
	data["schema_version"] = "99"
	path := writeTempProfile(t, data)

	_, err := model.ParseProfile(path)
	if !model.IsErrProfileVersionMismatch(err) {
		t.Errorf("expected ErrProfileVersionMismatch, got: %T: %v", err, err)
	}
	if err != nil && !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("error message should mention 'upgrade', got: %v", err)
	}
}

func TestParseProfile_MissingOperationsKey_ReturnsErrProfileMalformed(t *testing.T) {
	data := map[string]any{
		"schema_version": "1",
		"generated_at":   "2024-01-15T10:00:00Z",
	}
	path := writeTempProfile(t, data)
	_, err := model.ParseProfile(path)
	if !model.IsErrProfileMalformed(err) {
		t.Errorf("expected ErrProfileMalformed for missing 'operations', got: %T: %v", err, err)
	}
}

func TestParseProfile_DistributionDoesNotSumToOne_ReturnsErrProfileMalformed(t *testing.T) {
	data := validProfileData()
	data["operations"].(map[string]any)["CreateOrder"].(map[string]any)["arguments"].(map[string]any)["currency"].(map[string]any)["distribution"] = map[string]float64{
		"GBP": 1.0,
		"USD": 0.50,
	}
	path := writeTempProfile(t, data)
	_, err := model.ParseProfile(path)
	if !model.IsErrProfileMalformed(err) {
		t.Errorf("expected ErrProfileMalformed for non-normalised distribution, got: %T: %v", err, err)
	}
}

func TestTraceProfile_WriteToFile_RoundTrips(t *testing.T) {
	original := &model.TraceProfile{
		SchemaVersion: "1",
		GeneratedAt:   "2024-01-15T10:00:00Z",
		SourceHAR:     "sample.har",
		Operations: map[string]*model.OperationProfile{
			"CreateOrder": {
				CallCount:     30,
				FrequencyRank: 1,
				Arguments: map[string]*model.ArgumentProfile{
					"currency": {
						Type:          "string",
						Samples:       []any{"GBP", "USD"},
						Distribution:  map[string]float64{"GBP": 0.70, "USD": 0.30},
						AlwaysPresent: true,
					},
				},
			},
		},
		Sequences: &model.SequenceProfile{
			StartProbability:         map[string]float64{"CreateOrder": 1.0},
			Transitions:              map[string]map[string]float64{"CreateOrder": {"__END__": 1.0}},
			MinObservedSessionLength: 1,
			MaxObservedSessionLength: 1,
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, ".bt", "trace", "profile.json")

	if err := original.WriteToFile(path); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}

	t.Run("file exists", func(t *testing.T) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("expected file to exist after WriteToFile")
		}
	})

	t.Run("parent directories are created", func(t *testing.T) {
		if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
			t.Error("expected parent directories to be created by WriteToFile")
		}
	})

	parsed, err := model.ParseProfile(path)
	if err != nil {
		t.Fatalf("ParseProfile after WriteToFile: %v", err)
	}

	t.Run("schema version survives round-trip", func(t *testing.T) {
		if parsed.SchemaVersion != original.SchemaVersion {
			t.Errorf("want %q, got %q", original.SchemaVersion, parsed.SchemaVersion)
		}
	})
	t.Run("operation count survives round-trip", func(t *testing.T) {
		if len(parsed.Operations) != len(original.Operations) {
			t.Errorf("want %d operations, got %d", len(original.Operations), len(parsed.Operations))
		}
	})
	t.Run("distribution survives round-trip", func(t *testing.T) {
		arg := parsed.Operations["CreateOrder"].Arguments["currency"]
		if arg.Distribution["GBP"] != 0.70 {
			t.Errorf("GBP: want 0.70, got %f", arg.Distribution["GBP"])
		}
	})
}
