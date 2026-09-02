package report_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/report"
	"github.com/jimbery/bt/pkg/model"
)

func passedResult(id string) model.Result {
	return model.Result{
		CaseID:           id,
		Passed:           true,
		StatusCode:       200,
		SchemaViolations: []model.SchemaViolation{},
	}
}

func failedResultWithViolation(id string) model.Result {
	return model.Result{
		CaseID:     id,
		Passed:     false,
		StatusCode: 200,
		SchemaViolations: []model.SchemaViolation{
			{
				Field:        "$.status",
				ExpectedType: "string",
				ActualType:   "null",
				Message:      "required field is absent",
				Severity:     model.SeverityCritical,
			},
		},
	}
}

func TestConsoleReporter_PassedCase_NoViolationDetail(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write([]model.Result{passedResult("health-check")}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "PASS") {
		t.Errorf("want PASS: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "required field is absent") {
		t.Errorf("unexpected violation detail: %s", out)
	}
}

func TestConsoleReporter_SchemaViolation_RenderedWithFieldAndSeverity(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write([]model.Result{failedResultWithViolation("get-order-missing-status")}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "FAIL") {
		t.Errorf("want FAIL: %s", out)
	}
	if !strings.Contains(out, "$.status") {
		t.Errorf("want field path: %s", out)
	}
	if !strings.Contains(out, "Critical") {
		t.Errorf("want Critical: %s", out)
	}
	if !strings.Contains(out, "required field is absent") {
		t.Errorf("want message: %s", out)
	}
}

func TestConsoleReporter_SummaryLine_IncludesViolationCount(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write([]model.Result{
		passedResult("case-1"),
		failedResultWithViolation("case-2"),
	}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "schema violation") {
		t.Errorf("want summary schema violation text: %s", out)
	}
}

func TestJSONReporter_SchemaViolations_IncludedInOutput(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewJSON(&buf)
	if err := r.Write([]model.Result{failedResultWithViolation("get-order-missing-status")}); err != nil {
		t.Fatal(err)
	}
	var top struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &top); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	if len(top.Results) != 1 {
		t.Fatalf("results len %d", len(top.Results))
	}
	violations, ok := top.Results[0]["schema_violations"].([]any)
	if !ok {
		t.Fatalf("schema_violations type %T", top.Results[0]["schema_violations"])
	}
	if len(violations) != 1 {
		t.Fatalf("violations len %d", len(violations))
	}
	v := violations[0].(map[string]any)
	if v["field"] != "$.status" {
		t.Errorf("field=%v", v["field"])
	}
	if v["severity"] != "Critical" {
		t.Errorf("severity=%v", v["severity"])
	}
	if v["message"] == "" || v["message"] == "schema mismatch" {
		t.Errorf("message=%v", v["message"])
	}
}

func TestJSONReporter_NoViolations_EmptyArray(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewJSON(&buf)
	if err := r.Write([]model.Result{passedResult("health-check")}); err != nil {
		t.Fatal(err)
	}
	var top struct {
		Results []map[string]any `json:"results"`
	}
	if err := json.Unmarshal(buf.Bytes(), &top); err != nil {
		t.Fatal(err)
	}
	violations := top.Results[0]["schema_violations"]
	arr, ok := violations.([]any)
	if !ok {
		t.Fatalf("type %T", violations)
	}
	if len(arr) != 0 {
		t.Errorf("want empty array, got %v", arr)
	}
}

func TestJUnitReporter_SchemaViolation_RenderedAsFailureElement(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewJUnit(&buf)
	if err := r.Write([]model.Result{failedResultWithViolation("get-order-missing-status")}); err != nil {
		t.Fatal(err)
	}
	xml := buf.String()
	if !strings.Contains(xml, "<failure") {
		t.Errorf("want failure element: %s", xml)
	}
	if !strings.Contains(xml, "$.status") {
		t.Errorf("want field in xml: %s", xml)
	}
	if !strings.Contains(xml, "required field is absent") {
		t.Errorf("want message in xml: %s", xml)
	}
}
