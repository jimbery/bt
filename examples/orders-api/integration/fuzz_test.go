//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

type fuzzReport struct {
	Results []fuzzResult `json:"results"`
}

type fuzzResult struct {
	CaseID        string        `json:"case_id"`
	StrategyKind  string        `json:"strategy_kind"`
	Skipped       bool          `json:"skipped"`
	SkipReason    string        `json:"skip_reason"`
	MutationCount int           `json:"mutation_count"`
	Failures      []fuzzFailure `json:"failures"`
}

type fuzzFailure struct {
	Classification string `json:"classification"`
	Message        string `json:"message"`
	MutatedInput   string `json:"mutated_input"`
	ArtifactPath   string `json:"artifact_path"`
}

func runFuzzTests(t *testing.T) fuzzReport {
	t.Helper()
	requireOrdersAPI(t)

	beforeDeletes := getDeleteCount(t)

	root := findRepoRoot(t)
	bin := filepath.Join(root, "bt")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("bt binary not present; build with: go build -o bt ./cmd/bt")
	}

	reportFile := filepath.Join(t.TempDir(), "fuzz-report.json")
	cmd := exec.Command(
		bin,
		"run",
		"--config", filepath.Join(root, "examples/orders-api/bt/backendtest-fuzz.yaml"),
		"--strategy", "fuzz",
		"--safety", "safe",
		"--fuzz-iterations", "40",
		"--output", "json",
		"--output-file", reportFile,
	)
	cmd.Dir = root
	_ = cmd.Run()

	data, err := os.ReadFile(reportFile)
	if err != nil {
		t.Fatalf("cannot read fuzz report: %v", err)
	}
	var report fuzzReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("cannot parse fuzz report: %v\nraw: %s", err, data)
	}

	afterDeletes := getDeleteCount(t)
	if delta := afterDeletes - beforeDeletes; delta != 0 {
		t.Fatalf("safe fuzz profile must not send DELETE to the API; delete_attempts delta=%d (before=%d after=%d)", delta, beforeDeletes, afterDeletes)
	}

	return report
}

var fuzzReportOnce sync.Once
var fuzzReportCached fuzzReport

// cachedFuzzReport runs fuzz once per package test run; all assertions share the result.
func cachedFuzzReport(t *testing.T) fuzzReport {
	t.Helper()
	fuzzReportOnce.Do(func() {
		fuzzReportCached = runFuzzTests(t)
	})
	return fuzzReportCached
}

func getDeleteCount(t *testing.T) int {
	t.Helper()
	resp, err := http.Get("http://localhost:8080/admin/delete-count")
	if err != nil {
		t.Fatalf("cannot reach /admin/delete-count: %v", err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw := body["delete_attempts"]
	switch v := raw.(type) {
	case float64:
		return int(v)
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			t.Fatalf("delete_attempts json.Number: %v", err)
		}
		return int(i)
	case int:
		return v
	case int64:
		return int(v)
	default:
		t.Fatalf("delete_attempts type %T", raw)
		return 0
	}
}

func TestFuzzIntegration_Results_HaveStrategyKindFuzz(t *testing.T) {
	report := cachedFuzzReport(t)
	for _, r := range report.Results {
		if r.StrategyKind != "fuzz" {
			t.Errorf("result %q has StrategyKind=%q, expected 'fuzz'", r.CaseID, r.StrategyKind)
		}
	}
}

func TestFuzzIntegration_Failures_HaveClassificationSet(t *testing.T) {
	report := cachedFuzzReport(t)
	for _, r := range report.Results {
		for _, f := range r.Failures {
			if f.Classification == "" {
				t.Errorf("failure in %q has empty Classification", r.CaseID)
			}
		}
	}
}

func TestFuzzIntegration_Failures_HaveArtifactPath(t *testing.T) {
	report := cachedFuzzReport(t)
	for _, r := range report.Results {
		for _, f := range r.Failures {
			if f.ArtifactPath == "" {
				t.Errorf("failure in %q (classification=%s) has no ArtifactPath", r.CaseID, f.Classification)
			}
			if _, err := os.Stat(f.ArtifactPath); err != nil {
				t.Errorf("artifact %q referenced in report does not exist on disk: %v", f.ArtifactPath, err)
			}
		}
	}
}

func TestFuzzIntegration_Failures_HaveMutatedInput(t *testing.T) {
	report := cachedFuzzReport(t)
	for _, r := range report.Results {
		for _, f := range r.Failures {
			if f.MutatedInput == "" {
				t.Errorf("failure in %q has empty MutatedInput", r.CaseID)
			}
		}
	}
}

func TestFuzzIntegration_MutationCount_IsPositive(t *testing.T) {
	report := cachedFuzzReport(t)
	for _, r := range report.Results {
		if !r.Skipped && r.MutationCount <= 0 {
			t.Errorf("result %q has MutationCount=%d — expected > 0 for non-skipped operations", r.CaseID, r.MutationCount)
		}
	}
}

func TestFuzzIntegration_SkippedResults_HaveSkipReason(t *testing.T) {
	report := cachedFuzzReport(t)
	for _, r := range report.Results {
		if r.Skipped && r.SkipReason == "" {
			t.Errorf("skipped result %q has no SkipReason", r.CaseID)
		}
	}
}

func TestFuzzIntegration_BrokenEndpoint_FindsSchemaBreak(t *testing.T) {
	report := cachedFuzzReport(t)
	found := false
	for _, r := range report.Results {
		if r.CaseID == "fuzz:GetOrderBroken" {
			for _, f := range r.Failures {
				if f.Classification == "schema_break" {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected fuzz to find a schema_break on GetOrderBroken within the iteration budget")
	}
}
