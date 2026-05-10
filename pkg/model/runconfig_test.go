package model_test

import (
	"testing"

	"github.com/jayimbery/bt/pkg/model"
)

func TestRunConfigFromMap_RoundTrip(t *testing.T) {
	cfg := map[string]any{
		"fuzz_iterations": 42,
		"corpus_dir":      "/tmp/corpus",
		"safety": map[string]any{
			"profile":                 "aggressive",
			"allowed_methods":         []any{"GET"},
			"denied_methods":          []any{"DELETE"},
			"max_requests_per_second": 5.0,
			"max_concurrency":         2,
			"timeout_seconds":         12.0,
		},
	}
	rc := model.RunConfigFromMap(cfg)
	if rc.FuzzIterations != 42 {
		t.Fatalf("FuzzIterations: got %d", rc.FuzzIterations)
	}
	if rc.CorpusDir != "/tmp/corpus" {
		t.Fatalf("CorpusDir: got %q", rc.CorpusDir)
	}
	if rc.Safety.Profile != "aggressive" {
		t.Fatalf("Safety.Profile: got %q", rc.Safety.Profile)
	}
	if len(rc.Safety.AllowedMethods) != 1 || rc.Safety.AllowedMethods[0] != "GET" {
		t.Fatalf("AllowedMethods: %+v", rc.Safety.AllowedMethods)
	}
	m := model.RunConfigToMap(rc)
	rc2 := model.RunConfigFromMap(m)
	if rc2.FuzzIterations != rc.FuzzIterations || rc2.CorpusDir != rc.CorpusDir {
		t.Fatalf("round trip mismatch: %+v vs %+v", rc, rc2)
	}
}
