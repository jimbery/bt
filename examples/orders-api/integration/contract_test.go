//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jimbery/bt/internal/testutil"
)

// ContractRunResult mirrors the enriched JSON report from bt run --output json.
type ContractRunResult struct {
	Summary struct {
		Total       int `json:"total"`
		Passed      int `json:"passed"`
		Failed      int `json:"failed"`
		Quarantined int `json:"quarantined"`
		Skipped     int `json:"skipped"`
	} `json:"summary"`
	Results []struct {
		OperationID string `json:"operation_id"`
		Passed      bool   `json:"passed"`
		Quarantined bool   `json:"quarantined"`
		Violations  []struct {
			Field    string `json:"field"`
			Expected string `json:"expected"`
			Actual   string `json:"actual"`
			Severity string `json:"severity"`
		} `json:"violations"`
		SchemaPath string `json:"schema_path"`
	} `json:"results"`
}

func btBinaryContract(t *testing.T) string {
	t.Helper()
	root := testutil.RepoRoot(t)
	bin := filepath.Join(root, "bt")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("bt binary not present; build with: go build -o bt ./cmd/bt")
	}
	return bin
}

func runContractJSON(t *testing.T) []byte {
	t.Helper()
	requireOrdersAPI(t)
	root := testutil.RepoRoot(t)
	bt := btBinaryContract(t)
	cmd := exec.Command(bt, "run",
		"--config", "examples/orders-api/bt/backendtest.yaml",
		"--strategy", "contract",
		"--output", "json",
	)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Logf("bt run contract: exit %d stderr=%s", ee.ExitCode(), string(ee.Stderr))
		}
		t.Fatalf("bt run contract: %v", err)
	}
	return out
}

func TestContractRun_JSONOutputIsValidSchema(t *testing.T) {
	out := runContractJSON(t)

	var result ContractRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}
	s := result.Summary
	if s.Total < 0 {
		t.Errorf("summary.total must be >= 0, got %d", s.Total)
	}
	if s.Passed+s.Failed+s.Quarantined+s.Skipped != s.Total {
		t.Errorf("passed(%d)+failed(%d)+quarantined(%d)+skipped(%d) must equal total(%d)",
			s.Passed, s.Failed, s.Quarantined, s.Skipped, s.Total)
	}
}

func TestContractRun_EachResultHasRequiredFields(t *testing.T) {
	out := runContractJSON(t)

	var result ContractRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(result.Results) == 0 {
		t.Fatal("expected at least one result")
	}
	for i, r := range result.Results {
		if r.OperationID == "" {
			t.Errorf("result[%d]: operation_id must be non-empty", i)
		}
		if r.SchemaPath == "" {
			t.Errorf("result[%d] (%s): schema_path must be non-empty", i, r.OperationID)
		}
	}
}

func TestContractRun_ViolationsHaveFullFieldPaths(t *testing.T) {
	out := runContractJSON(t)

	var result ContractRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, r := range result.Results {
		for j, v := range r.Violations {
			if v.Field == "" {
				t.Errorf("result %s violation[%d]: Field must be non-empty", r.OperationID, j)
			}
			if v.Expected == "" {
				t.Errorf("result %s violation[%d]: Expected must be non-empty", r.OperationID, j)
			}
			if v.Actual == "" {
				t.Errorf("result %s violation[%d]: Actual must be non-empty", r.OperationID, j)
			}
			if v.Severity != "critical" && v.Severity != "warning" {
				t.Errorf("result %s violation[%d]: Severity must be critical or warning, got %q", r.OperationID, j, v.Severity)
			}
		}
	}
}

func TestContractRun_GetOrderBrokenIsQuarantined(t *testing.T) {
	out := runContractJSON(t)

	var result ContractRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, r := range result.Results {
		if r.OperationID != "GetOrderBroken" {
			continue
		}
		if !r.Quarantined {
			t.Error("GetOrderBroken must be marked quarantined in the report")
		}
		if len(r.Violations) == 0 {
			t.Error("GetOrderBroken must report violations when quarantined")
		}
		for j, v := range r.Violations {
			if v.Field == "" {
				t.Errorf("GetOrderBroken violation[%d]: Field must be non-empty", j)
			}
		}
		return
	}
	t.Error("GetOrderBroken not found in contract run results")
}

func TestContractRun_WellFormedOperationsPass(t *testing.T) {
	out := runContractJSON(t)

	var result ContractRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	expectedPass := map[string]bool{
		"GetHealth":   false,
		"ListOrders":  false,
		"CreateOrder": false,
		"GetOrder":    false,
	}

	for _, r := range result.Results {
		if _, expected := expectedPass[r.OperationID]; expected {
			expectedPass[r.OperationID] = true
			if !r.Passed && !r.Quarantined {
				for _, v := range r.Violations {
					t.Errorf("operation %s violation field=%q expected=%q actual=%q severity=%s",
						r.OperationID, v.Field, v.Expected, v.Actual, v.Severity)
				}
				if len(r.Violations) == 0 {
					t.Errorf("operation %s expected to pass but failed (no violations in JSON)", r.OperationID)
				}
			}
		}
	}

	for opID, seen := range expectedPass {
		if !seen {
			t.Errorf("expected operation %q in contract results, not found", opID)
		}
	}
}

func TestContractRun_ExitCodeZeroWhenOnlyQuarantinedFailures(t *testing.T) {
	requireOrdersAPI(t)
	root := testutil.RepoRoot(t)
	bt := btBinaryContract(t)
	cmd := exec.Command(bt, "run",
		"--config", "examples/orders-api/bt/backendtest.yaml",
		"--strategy", "contract",
	)
	cmd.Dir = root
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				t.Errorf("exit code 1: a non-quarantined contract violation was detected")
			} else {
				t.Errorf("unexpected exit code %d: %v", exitErr.ExitCode(), err)
			}
		} else {
			t.Errorf("unexpected error: %v", err)
		}
	}
}
