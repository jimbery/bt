//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found from", dir)
		}
		dir = parent
	}
}

func requireOrdersAPI(t *testing.T) {
	t.Helper()
	bases := []string{}
	if p := strings.TrimSpace(os.Getenv("PORT")); p != "" {
		bases = append(bases, "http://127.0.0.1:"+p, "http://localhost:"+p)
	}
	bases = append(bases, "http://localhost:8080", "http://127.0.0.1:18080")

	seen := make(map[string]struct{})
	for _, base := range bases {
		if _, dup := seen[base]; dup {
			continue
		}
		seen[base] = struct{}{}
		resp, err := http.Get(base + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
	}
	t.Skip("orders-api not reachable (set PORT or start on :8080 or :18080)")
}

// propertyReport matches the JSON output format from bt run --output json.
type propertyReport struct {
	Results []propertyResult `json:"results"`
}

type propertyResult struct {
	CaseID       string    `json:"case_id"`
	StrategyKind string    `json:"strategy_kind"`
	Seed         int64     `json:"seed"`
	CasesRun     int       `json:"cases_run"`
	ShrinkCount  int       `json:"shrink_count"`
	Failures     []failure `json:"failures"`
	ArtifactPath string    `json:"artifact_path"`
}

type failure struct {
	Invariant string `json:"invariant"`
	Message   string `json:"message"`
	Path      string `json:"path"`
	Expected  any    `json:"expected"`
	Actual    any    `json:"actual"`
}

func runPropertyTests(t *testing.T) propertyReport {
	t.Helper()
	requireOrdersAPI(t)

	root := findRepoRoot(t)
	bin := filepath.Join(root, "bt")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("bt binary not present; build with: go build -o bt ./cmd/bt")
	}

	reportFile := filepath.Join(t.TempDir(), "property-report.json")
	cmd := exec.Command(
		bin,
		"run",
		"--config", "examples/orders-api/bt/backendtest-property.yaml",
		"--strategy", "property",
		"--output", "json",
		"--output-file", reportFile,
	)
	cmd.Dir = root
	_ = cmd.Run()

	data, err := os.ReadFile(reportFile)
	if err != nil {
		t.Fatalf("cannot read property report: %v", err)
	}

	var report propertyReport
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatalf("cannot parse property report JSON: %v\nraw: %s", err, data)
	}
	return report
}

func TestPropertyIntegration_BrokenEndpoint_FoundAutomatically(t *testing.T) {
	report := runPropertyTests(t)

	found := false
	for _, r := range report.Results {
		if !strings.Contains(r.CaseID, "GetOrderBroken") {
			continue
		}
		for _, f := range r.Failures {
			if f.Invariant == "response_matches_schema" {
				found = true
			}
		}
	}

	if !found {
		t.Error("expected property testing to find a response_matches_schema failure on the broken endpoint automatically")
	}
}

func TestPropertyIntegration_BrokenEndpoint_ViolationHasPath(t *testing.T) {
	report := runPropertyTests(t)

	for _, r := range report.Results {
		for _, f := range r.Failures {
			if f.Invariant == "response_matches_schema" {
				if f.Path == "" {
					t.Errorf("schema violation has empty path — expected a JSON path like '$.amount': %+v", f)
				}
				return
			}
		}
	}
	t.Error("no response_matches_schema failures found to check path on")
}

func TestPropertyIntegration_BrokenEndpoint_ViolationHasExpectedAndActual(t *testing.T) {
	report := runPropertyTests(t)

	for _, r := range report.Results {
		for _, f := range r.Failures {
			if f.Invariant == "response_matches_schema" {
				if fmt.Sprint(f.Expected) == "" {
					t.Errorf("schema violation at %q has empty Expected field", f.Path)
				}
				return
			}
		}
	}
	t.Error("no response_matches_schema failures found to check Expected/Actual on")
}

func TestPropertyIntegration_BrokenEndpoint_ArtifactIsWritten(t *testing.T) {
	report := runPropertyTests(t)

	for _, r := range report.Results {
		if len(r.Failures) > 0 && r.ArtifactPath != "" {
			if _, err := os.Stat(r.ArtifactPath); err != nil {
				t.Errorf("artifact path %q referenced in report but file does not exist: %v", r.ArtifactPath, err)
			}
			return
		}
	}
	t.Error("no failing result with an artifact path was found in the report")
}

func TestPropertyIntegration_PropertyResults_SeedIsPresent(t *testing.T) {
	report := runPropertyTests(t)

	for _, r := range report.Results {
		if r.StrategyKind == "property" {
			if r.Seed == 0 {
				t.Errorf("property result %q has seed 0 — seed must be set even for random runs", r.CaseID)
			}
		}
	}
}

func TestPropertyIntegration_PropertyResults_CasesRunIsPositive(t *testing.T) {
	report := runPropertyTests(t)

	for _, r := range report.Results {
		if r.StrategyKind == "property" {
			if r.CasesRun <= 0 {
				t.Errorf("property result %q has CasesRun=%d — expected > 0", r.CaseID, r.CasesRun)
			}
		}
	}
}

func TestPropertyIntegration_BrokenEndpoint_ShrinkCountIsPositive(t *testing.T) {
	report := runPropertyTests(t)

	for _, r := range report.Results {
		for _, f := range r.Failures {
			if f.Invariant == "response_matches_schema" {
				if r.ShrinkCount <= 0 {
					t.Errorf("expected shrink count > 0 for a found failure, got %d", r.ShrinkCount)
				}
				return
			}
		}
	}
	t.Error("no response_matches_schema failures found to check shrink count on")
}

func TestPropertyIntegration_PassingRun_ReplaySeed(t *testing.T) {
	report := runPropertyTests(t)

	var seed int64
	for _, r := range report.Results {
		if len(r.Failures) > 0 && r.Seed != 0 {
			seed = r.Seed
			break
		}
	}
	if seed == 0 {
		t.Skip("no failing result with a seed — skipping replay determinism check")
	}

	root := findRepoRoot(t)
	bin := filepath.Join(root, "bt")
	reportFile := filepath.Join(t.TempDir(), "replay-report.json")
	cmd := exec.Command(
		bin,
		"run",
		"--config", "examples/orders-api/bt/backendtest-property.yaml",
		"--strategy", "property",
		"--seed", fmt.Sprintf("%d", seed),
		"--output", "json",
		"--output-file", reportFile,
	)
	cmd.Dir = root
	_ = cmd.Run()

	data, err := os.ReadFile(reportFile)
	if err != nil {
		t.Fatalf("cannot read replay report: %v", err)
	}

	var replay propertyReport
	if err := json.Unmarshal(data, &replay); err != nil {
		t.Fatalf("cannot parse replay report: %v", err)
	}

	foundFailure := false
	for _, r := range replay.Results {
		if len(r.Failures) > 0 {
			foundFailure = true
		}
	}
	if !foundFailure {
		t.Error("seeded replay did not reproduce the failure found in the initial run")
	}
}
