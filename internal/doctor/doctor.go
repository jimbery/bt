package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jayimbery/bt/internal/strategy/contract"
)

// Level classifies a doctor check outcome.
type Level int

const (
	Pass Level = iota
	Fail
	Warn
)

// CheckResult is one diagnostic row.
type CheckResult struct {
	ID         string
	Passed     bool
	Level      Level
	Message    string
	DurationMs int64
}

// Config drives RunAll.
type Config struct {
	SchemaPath    string
	TargetBaseURL string // e.g. https://localhost:8080 — doctor probes TargetBaseURL + "/health"
	AuthEnvVar    string // if non-empty, env must be set and non-empty
	BaselinePath  string
}

// CheckSchemaReachable verifies the schema file exists and is readable.
func CheckSchemaReachable(schemaPath string) CheckResult {
	id := "schema-reachable"
	if schemaPath == "" {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: "schema path is empty"}
	}
	if st, err := os.Stat(schemaPath); err != nil {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: fmt.Sprintf("schema not found or unreadable at %s: %v", schemaPath, err)}
	} else if st.IsDir() {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: fmt.Sprintf("schema path %s is a directory, expected a file", schemaPath)}
	}
	abs, _ := filepath.Abs(schemaPath)
	return CheckResult{ID: id, Passed: true, Level: Pass, Message: fmt.Sprintf("schema found at %s", abs)}
}

// CheckTargetReachable performs GET {base}/health with a 5s client timeout.
func CheckTargetReachable(baseURL string) CheckResult {
	id := "target-reachable"
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: "target base URL is empty"}
	}
	u := base + "/health"
	start := time.Now()
	client := client5s()
	resp, err := client.Get(u)
	dur := time.Since(start).Milliseconds()
	if err != nil {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: fmt.Sprintf("GET %s failed: %v", u, err), DurationMs: dur}
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: fmt.Sprintf("GET %s returned HTTP %d", u, resp.StatusCode), DurationMs: dur}
	}
	return CheckResult{ID: id, Passed: true, Level: Pass, Message: fmt.Sprintf("GET %s → %d in %dms", u, resp.StatusCode, dur), DurationMs: dur}
}

// CheckAuthConfigured verifies an auth env var when required.
func CheckAuthConfigured(envVarName string) CheckResult {
	id := "auth-configured"
	if strings.TrimSpace(envVarName) == "" {
		return CheckResult{ID: id, Passed: true, Level: Warn, Message: "auth not required by config (no env var specified)"}
	}
	v := os.Getenv(envVarName)
	if strings.TrimSpace(v) == "" {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: fmt.Sprintf("%s is not set or empty (required by config)", envVarName)}
	}
	return CheckResult{ID: id, Passed: true, Level: Pass, Message: fmt.Sprintf("%s is set", envVarName)}
}

// CheckBaselineValid parses baseline YAML when the file exists.
func CheckBaselineValid(path string) CheckResult {
	id := "baseline-valid"
	if strings.TrimSpace(path) == "" {
		return CheckResult{ID: id, Passed: true, Level: Warn, Message: "no baseline path configured"}
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return CheckResult{ID: id, Passed: true, Level: Warn, Message: fmt.Sprintf("no baseline file at %s (optional)", path)}
		}
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: fmt.Sprintf("cannot stat baseline %s: %v", path, err)}
	}
	b, err := contract.LoadBaseline(path)
	if err != nil {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: fmt.Sprintf("baseline %s: %v", path, err)}
	}
	n := len(b.Quarantined)
	entryWord := "entries"
	if n == 1 {
		entryWord = "entry"
	}
	return CheckResult{ID: id, Passed: true, Level: Pass, Message: fmt.Sprintf("%s parsed cleanly (%d quarantined %s)", path, n, entryWord)}
}

// CheckGoVersion verifies the running Go toolchain is recent enough for bt.
func CheckGoVersion() CheckResult {
	id := "go-version"
	v := goVersionString()
	ok, msg := goAtLeast121(v)
	if !ok {
		return CheckResult{ID: id, Passed: false, Level: Fail, Message: msg}
	}
	return CheckResult{ID: id, Passed: true, Level: Pass, Message: msg}
}

// RunAll executes all five checks in order.
func RunAll(cfg Config) []CheckResult {
	return []CheckResult{
		CheckSchemaReachable(cfg.SchemaPath),
		CheckTargetReachable(cfg.TargetBaseURL),
		CheckAuthConfigured(cfg.AuthEnvVar),
		CheckBaselineValid(cfg.BaselinePath),
		CheckGoVersion(),
	}
}

func jsonLevel(l Level) string {
	switch l {
	case Pass:
		return "pass"
	case Fail:
		return "fail"
	case Warn:
		return "warn"
	default:
		return "unknown"
	}
}

type doctorJSONCheck struct {
	ID         string `json:"id"`
	Passed     bool   `json:"passed"`
	Level      string `json:"level"`
	Message    string `json:"message"`
	DurationMs int64  `json:"duration_ms,omitempty"`
}

// FormatJSON renders doctor checks as JSON for machine-readable output.
func FormatJSON(results []CheckResult) ([]byte, error) {
	out := struct {
		Checks []doctorJSONCheck `json:"checks"`
	}{}
	for _, r := range results {
		out.Checks = append(out.Checks, doctorJSONCheck{
			ID:         r.ID,
			Passed:     r.Passed,
			Level:      jsonLevel(r.Level),
			Message:    r.Message,
			DurationMs: r.DurationMs,
		})
	}
	return json.MarshalIndent(&out, "", "  ")
}

// FormatConsole renders results as a short text report.
func FormatConsole(title string, results []CheckResult) string {
	var b strings.Builder
	b.WriteString(title)
	b.WriteString("\n\n")
	fail := 0
	for _, r := range results {
		icon := "✓"
		if r.Level == Warn {
			icon = "!"
		}
		if !r.Passed && r.Level == Fail {
			icon = "✗"
			fail++
		}
		b.WriteString(fmt.Sprintf("  %s  %-18s %s\n", icon, r.ID, r.Message))
	}
	b.WriteString("\n")
	if fail > 0 {
		b.WriteString(fmt.Sprintf("%d check(s) failed. Fix the issues above before running bt.\n", fail))
	} else {
		b.WriteString("All blocking checks passed.\n")
	}
	return b.String()
}
