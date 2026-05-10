package contract_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/strategy/contract"
)

func writeBaseline(t *testing.T, dir, content string) string {
	t.Helper()
	path := filepath.Join(dir, "baseline.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeBaseline: %v", err)
	}
	return path
}

func stubContract(opID string, passed bool) contract.ContractResult {
	return contract.ContractResult{
		OperationID: opID,
		Passed:      passed,
		Violations:  []contract.ContractViolation{{Field: "status", Severity: contract.Critical}},
	}
}

func TestBaseline_QuarantinedOperation_MarkedAsQuarantinedNotFailed(t *testing.T) {
	dir := t.TempDir()
	path := writeBaseline(t, dir, `
version: 1
quarantined:
  - operation_id: GetOrderBroken
    reason: "tracked in ISSUE-42"
`)

	baseline, err := contract.LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	result := stubContract("GetOrderBroken", false)

	annotated := baseline.Annotate(result)

	if annotated.Failed() {
		t.Error("quarantined result must not count as failed")
	}
	if !annotated.Quarantined {
		t.Error("quarantined result must have Quarantined=true")
	}
	if annotated.QuarantineReason == "" {
		t.Error("QuarantineReason must be non-empty")
	}
}

func TestBaseline_QuarantineExpired_TreatedAsLiveFailure(t *testing.T) {
	dir := t.TempDir()
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	path := writeBaseline(t, dir, `
version: 1
quarantined:
  - operation_id: GetOrderBroken
    reason: "should have been fixed by now"
    quarantine_until: "`+yesterday+`"
`)

	baseline, err := contract.LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	result := stubContract("GetOrderBroken", false)

	annotated := baseline.Annotate(result)

	if !annotated.Failed() {
		t.Error("expired quarantine must treat the result as a live failure")
	}
	if !annotated.QuarantineExpired {
		t.Error("QuarantineExpired must be true for an expired quarantine")
	}
}

func TestBaseline_StaleEntry_OperationNowPasses_WarningFlagged(t *testing.T) {
	dir := t.TempDir()
	path := writeBaseline(t, dir, `
version: 1
quarantined:
  - operation_id: GetOrderBroken
    reason: "tracked in ISSUE-42"
`)

	baseline, err := contract.LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	passingResult := contract.ContractResult{
		OperationID: "GetOrderBroken",
		Passed:      true,
		Violations:  nil,
	}

	annotated := baseline.Annotate(passingResult)

	if !annotated.StaleBaseline {
		t.Error("StaleBaseline must be true when a quarantined operation now passes")
	}
}

func TestBaseline_UnknownOperation_NotAffected(t *testing.T) {
	dir := t.TempDir()
	path := writeBaseline(t, dir, `
version: 1
quarantined:
  - operation_id: GetOrderBroken
    reason: "tracked in ISSUE-42"
`)

	baseline, err := contract.LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}

	result := stubContract("GetHealth", false)

	annotated := baseline.Annotate(result)

	if !annotated.Failed() {
		t.Error("non-quarantined failure must still count as failed")
	}
	if annotated.Quarantined {
		t.Error("non-quarantined result must not be marked Quarantined")
	}
}

func TestBaseline_MissingFile_ReturnsError(t *testing.T) {
	_, err := contract.LoadBaseline("/no/such/file.yaml")
	if err == nil {
		t.Error("expected error for missing baseline file, got nil")
	}
}

func TestBaseline_MalformedYAML_ReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := writeBaseline(t, dir, `this is not yaml: [`)

	_, err := contract.LoadBaseline(path)
	if err == nil {
		t.Error("expected error for malformed YAML, got nil")
	}
}
