package report_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/report"
	"github.com/jimbery/bt/pkg/model"
)

func fuzzFailureResult() model.Result {
	return model.Result{
		CaseID:        "GetOrder",
		StrategyKind:  "fuzz",
		Passed:        false,
		StatusCode:    500,
		MutationCount: 10,
		Duration:      0,
		Failures: []model.Failure{
			{
				Invariant:      "fuzz",
				Classification: "crash",
				Message:        "connection dropped — no response received",
				MutatedInput:   "GET /orders/ord-001%00",
				ArtifactPath:   ".bt/artifacts/2026-05-09T160000Z-GetOrder-crash.json",
			},
			{
				Invariant:      "fuzz",
				Classification: "validation_leak",
				Message:        "body contains internal path: /var/app/config.yaml",
				MutatedInput:   "GET /orders/' OR 1=1--",
				ArtifactPath:   ".bt/artifacts/2026-05-09T160000Z-GetOrder-leak.json",
			},
		},
	}
}

func fuzzSkippedResult() model.Result {
	return model.Result{
		CaseID:       "DeleteOrder",
		StrategyKind: "fuzz",
		Skipped:      true,
		SkipReason:   "method blocked by safety profile (safe)",
	}
}

func TestFuzzReporter_Failure_PrintsFAIL(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), "FAIL") {
		t.Error("expected 'FAIL' in fuzz failure output")
	}
}

func TestFuzzReporter_Failure_PrintsStrategyKind(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), "fuzz") {
		t.Error("expected 'fuzz' strategy kind in output")
	}
}

func TestFuzzReporter_Failure_PrintsClassification(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	output := buf.String()
	if !strings.Contains(output, "crash") {
		t.Error("expected 'crash' classification in output")
	}
	if !strings.Contains(output, "validation_leak") {
		t.Error("expected 'validation_leak' classification in output")
	}
}

func TestFuzzReporter_Failure_PrintsMutatedInput(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), "/orders/ord-001%00") {
		t.Error("expected mutated input path in output")
	}
}

func TestFuzzReporter_Failure_PrintsArtifactPath(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), ".bt/artifacts/") {
		t.Error("expected artifact path in fuzz failure output")
	}
}

func TestFuzzReporter_Failure_PrintsMutationCount(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), "10") {
		t.Error("expected mutation count '10' in output")
	}
}

func TestFuzzReporter_Skipped_PrintsSKIP(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzSkippedResult()})
	if !strings.Contains(buf.String(), "SKIP") {
		t.Error("expected 'SKIP' for skipped operation")
	}
}

func TestFuzzReporter_Skipped_PrintsSkipReason(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzSkippedResult()})
	if !strings.Contains(buf.String(), "method blocked by safety profile") {
		t.Error("expected skip reason in output")
	}
}

func TestFuzzReporter_Summary_CountsSkippedSeparately(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{
		fuzzFailureResult(),
		fuzzSkippedResult(),
		{CaseID: "GetHealth", StrategyKind: "fuzz", Passed: true, StatusCode: 200, Failures: nil},
	})
	output := buf.String()
	if !strings.Contains(output, "skipped") {
		t.Error("expected 'skipped' in summary line")
	}
}
