//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// GraphQLRunResult mirrors the JSON report output of bt run --output json.
type GraphQLRunResult struct {
	Summary struct {
		Total       int `json:"total"`
		Passed      int `json:"passed"`
		Failed      int `json:"failed"`
		Skipped     int `json:"skipped"`
		Quarantined int `json:"quarantined"`
	} `json:"summary"`
	Results []struct {
		CaseID   string `json:"case_id"`
		Passed   bool   `json:"passed"`
		Failures []struct {
			Invariant string `json:"invariant"`
			Message   string `json:"message"`
			Path      string `json:"path"`
		} `json:"failures"`
		Response struct {
			StatusCode int             `json:"status_code"`
			Body       json.RawMessage `json:"body"` // object when JSON (GraphQL); string for raw non-JSON bodies
		} `json:"response"`
	} `json:"results"`
}

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

func btBinaryGraphQL(t *testing.T) string {
	t.Helper()
	root := findRepoRoot(t)
	bin := filepath.Join(root, "bt")
	if _, err := os.Stat(bin); err != nil {
		t.Skip("bt binary not present; build with: go build -o bt ./cmd/bt")
	}
	return bin
}

func defaultGraphQLConfig() string {
	if p := strings.TrimSpace(os.Getenv("GRAPHQL_BT_CONFIG")); p != "" {
		return p
	}
	return "examples/graphql-api/bt/backendtest.yaml"
}

func graphqlHealthURL(configPath string) string {
	if strings.HasSuffix(configPath, "backendtest-bug.yaml") {
		return "http://127.0.0.1:8091/health"
	}
	return "http://127.0.0.1:8090/health"
}

func requireGraphQLAPI(t *testing.T, configPath string) {
	t.Helper()
	u := graphqlHealthURL(configPath)
	for range 10 {
		resp, err := http.Get(u)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
	}
	t.Skipf("graphql-api not reachable at %s (start server for integration)", u)
}

func runGraphQLTableJSON(t *testing.T) ([]byte, string) {
	t.Helper()
	requireGraphQLAPI(t, defaultGraphQLConfig())
	root := findRepoRoot(t)
	bt := btBinaryGraphQL(t)
	cfg := defaultGraphQLConfig()
	cmd := exec.Command(bt, "run",
		"--config", cfg,
		"--strategy", "table",
		"--output", "json",
	)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Logf("bt run: exit %d stderr=%s", ee.ExitCode(), string(ee.Stderr))
		}
	}
	return out, cfg
}

func runGraphQLTableJSONWithConfig(t *testing.T, configRel string) []byte {
	t.Helper()
	requireGraphQLAPI(t, configRel)
	root := findRepoRoot(t)
	bt := btBinaryGraphQL(t)
	cmd := exec.Command(bt, "run",
		"--config", configRel,
		"--strategy", "table",
		"--output", "json",
	)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			t.Logf("bt run: exit %d stderr=%s", ee.ExitCode(), string(ee.Stderr))
		}
	}
	return out
}

func TestGraphQLRun_JSONOutputIsValidSchema(t *testing.T) {
	out, _ := runGraphQLTableJSON(t)

	var result GraphQLRunResult
	if jsonErr := json.Unmarshal(out, &result); jsonErr != nil {
		t.Fatalf("bt run produced invalid JSON: %v\noutput: %s", jsonErr, out)
	}

	s := result.Summary
	if s.Total < 0 {
		t.Errorf("summary.total must be >= 0, got %d", s.Total)
	}
	if s.Passed+s.Failed+s.Skipped+s.Quarantined != s.Total {
		t.Errorf("passed(%d)+failed(%d)+skipped(%d)+quarantined(%d) must equal total(%d)",
			s.Passed, s.Failed, s.Skipped, s.Quarantined, s.Total)
	}
}

func TestGraphQLRun_AllExpectedCasesPresent(t *testing.T) {
	out, _ := runGraphQLTableJSON(t)

	var result GraphQLRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	expectedCases := map[string]bool{
		"gql-health":                        false,
		"gql-orders-empty":                  false,
		"gql-create-order":                  false,
		"gql-create-order-missing-currency": false,
		"gql-order-not-found":               false,
	}

	for _, r := range result.Results {
		expectedCases[r.CaseID] = true
	}

	for id, seen := range expectedCases {
		if !seen {
			t.Errorf("expected case %q in results, not found", id)
		}
	}
}

func TestGraphQLRun_FailuresHavePaths(t *testing.T) {
	out, _ := runGraphQLTableJSON(t)

	var result GraphQLRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	for _, r := range result.Results {
		for j, f := range r.Failures {
			if f.Invariant != "response_matches_schema" {
				continue
			}
			if f.Path == "" {
				t.Errorf("case %s failure[%d]: path must be non-empty for response_matches_schema", r.CaseID, j)
			}
			if f.Message == "" {
				t.Errorf("case %s failure[%d]: Message must be non-empty", r.CaseID, j)
			}
		}
	}
}

func TestGraphQLRun_WellFormedCasesPass(t *testing.T) {
	out, _ := runGraphQLTableJSON(t)

	var result GraphQLRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	shouldPass := map[string]bool{
		"gql-health":          true,
		"gql-orders-empty":    true,
		"gql-create-order":    true,
		"gql-order-not-found": true,
	}

	for _, r := range result.Results {
		if !shouldPass[r.CaseID] {
			continue
		}
		if !r.Passed {
			t.Errorf("case %q expected to pass but failed. Failures:", r.CaseID)
			for _, f := range r.Failures {
				t.Errorf("  invariant=%q path=%q message=%q", f.Invariant, f.Path, f.Message)
			}
		}
	}
}

func TestGraphQLRun_AmountBugDetected(t *testing.T) {
	if os.Getenv("BT_GQL_AMOUNT_BUG") != "1" {
		t.Skip("BT_GQL_AMOUNT_BUG not set — skipping bug detection test")
	}

	cfg := strings.TrimSpace(os.Getenv("GRAPHQL_BT_CONFIG"))
	if cfg == "" {
		cfg = "examples/graphql-api/bt/backendtest-bug.yaml"
	}

	out := runGraphQLTableJSONWithConfig(t, cfg)

	var result GraphQLRunResult
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, out)
	}

	found := false
	for _, r := range result.Results {
		for _, f := range r.Failures {
			if f.Path != "" && strings.Contains(f.Path, "amount") {
				found = true
				t.Logf("Found amount violation in case %q: path=%q message=%q", r.CaseID, f.Path, f.Message)
			}
		}
	}
	if !found {
		t.Error("expected a schema violation on 'amount' when BT_GQL_AMOUNT_BUG=1, none found")
	}
}
