package report_test

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/jimbery/bt/internal/report"
	"github.com/jimbery/bt/pkg/model"
)

var sampleResults = []model.Result{
	{
		CaseID:     "get-order-200",
		Passed:     true,
		StatusCode: 200,
		Duration:   12 * time.Millisecond,
	},
	{
		CaseID:     "create-order-201",
		Passed:     false,
		StatusCode: 500,
		Duration:   8 * time.Millisecond,
		Failures: []model.Failure{
			{Invariant: "status_code", Message: "expected 201, got 500"},
		},
	},
}

func TestConsoleReporter_ContainsCaseIDs(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "get-order-200") {
		t.Error("expected output to contain case ID get-order-200")
	}
	if !strings.Contains(output, "create-order-201") {
		t.Error("expected output to contain case ID create-order-201")
	}
}

func TestConsoleReporter_ShowsPassFail(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "PASS") {
		t.Error("expected output to contain PASS")
	}
	if !strings.Contains(output, "FAIL") {
		t.Error("expected output to contain FAIL")
	}
}

func TestConsoleReporter_ShowsSummary(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewConsole(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "2") {
		t.Error("expected output to contain total count")
	}
}

func TestJSONReporter_ValidJSON(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewJSON(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
}

func TestJSONReporter_ContainsResults(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewJSON(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var out struct {
		Results []map[string]any `json:"results"`
		Summary map[string]any   `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("cannot unmarshal JSON output: %v", err)
	}
	if len(out.Results) != 2 {
		t.Errorf("expected 2 results in JSON output, got %d", len(out.Results))
	}
	if out.Summary == nil {
		t.Error("expected non-nil summary in JSON output")
	}
}

func TestJUnitReporter_ValidXML(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewJUnit(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var suites report.JUnitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &suites); err != nil {
		t.Fatalf("output is not valid JUnit XML: %v", err)
	}
}

func TestJUnitReporter_ContainsTestCases(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	r := report.NewJUnit(&buf)
	if err := r.Write(sampleResults); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var suites report.JUnitTestSuites
	if err := xml.Unmarshal(buf.Bytes(), &suites); err != nil {
		t.Fatalf("cannot unmarshal JUnit XML: %v", err)
	}
	if len(suites.Suites) == 0 {
		t.Fatal("expected at least one test suite")
	}
	suite := suites.Suites[0]
	if suite.Tests != 2 {
		t.Errorf("expected 2 tests, got %d", suite.Tests)
	}
	if suite.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", suite.Failures)
	}
}
