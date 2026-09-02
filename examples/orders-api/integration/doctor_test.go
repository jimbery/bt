//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/testutil"
)

type DoctorCheckResult struct {
	ID         string `json:"id"`
	Passed     bool   `json:"passed"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	DurationMs int64  `json:"duration_ms"`
}

type DoctorOutput struct {
	Checks []DoctorCheckResult `json:"checks"`
}

func btBinaryDoctor(t *testing.T) string {
	t.Helper()
	root := testutil.RepoRoot(t)
	bin := filepath.Join(root, "bt")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("bt binary not present; build with: go build -o bt ./cmd/bt")
	}
	return bin
}

func runDoctorJSON(t *testing.T) []byte {
	t.Helper()
	requireOrdersAPI(t)
	root := testutil.RepoRoot(t)
	bt := btBinaryDoctor(t)
	cmd := exec.Command(bt, "doctor",
		"--config", "examples/orders-api/bt/backendtest.yaml",
		"--output", "json",
	)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Logf("bt doctor: exit %d stderr=%s", ee.ExitCode(), string(ee.Stderr))
		}
		t.Fatalf("bt doctor: %v", err)
	}
	return out
}

func TestDoctor_AllChecksHaveIDAndMessage(t *testing.T) {
	out := runDoctorJSON(t)

	var result DoctorOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	if len(result.Checks) == 0 {
		t.Fatal("expected at least one check result")
	}
	for i, check := range result.Checks {
		if check.ID == "" {
			t.Errorf("check[%d]: ID must be non-empty", i)
		}
		if check.Message == "" {
			t.Errorf("check[%d] (%s): Message must be non-empty", i, check.ID)
		}
		if check.Level == "" {
			t.Errorf("check[%d] (%s): Level must be non-empty", i, check.ID)
		}
	}
}

func TestDoctor_ExactlyFiveChecksReturned(t *testing.T) {
	out := runDoctorJSON(t)

	var result DoctorOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Checks) != 5 {
		t.Fatalf("expected exactly 5 checks, got %d", len(result.Checks))
	}

	expected := []string{
		"schema-reachable",
		"target-reachable",
		"auth-configured",
		"baseline-valid",
		"go-version",
	}
	ids := make(map[string]bool)
	for _, c := range result.Checks {
		ids[c.ID] = true
	}
	for _, id := range expected {
		if !ids[id] {
			t.Errorf("expected check %q in doctor output, got ids %v", id, ids)
		}
	}
}

func TestDoctor_SchemaReachableCheck_PassesWhenSchemaPresent(t *testing.T) {
	out := runDoctorJSON(t)

	var result DoctorOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, check := range result.Checks {
		if check.ID != "schema-reachable" {
			continue
		}
		if !check.Passed {
			t.Errorf("schema-reachable failed: %s", check.Message)
		}
		if !strings.Contains(check.Message, "openapi") && !strings.Contains(check.Message, ".yaml") {
			t.Errorf("schema-reachable message should mention schema path, got: %q", check.Message)
		}
		return
	}
	t.Error("schema-reachable check not found")
}

func TestDoctor_TargetReachableCheck_ReportsDurationMs(t *testing.T) {
	out := runDoctorJSON(t)

	var result DoctorOutput
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, check := range result.Checks {
		if check.ID != "target-reachable" {
			continue
		}
		if !check.Passed {
			t.Logf("target-reachable failed: %s", check.Message)
			return
		}
		if !strings.Contains(check.Message, "ms") {
			t.Errorf("target-reachable message should report response time, got: %q", check.Message)
		}
		return
	}
	t.Error("target-reachable check not found")
}
