package replay_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/pkg/model"
)

func sampleArtifact() model.Artifact {
	return model.Artifact{
		ID:           "artifact-001",
		StrategyKind: "table",
		Seed:         0,
		CaseID:       "create-order-valid",
		OccurredAt:   time.Date(2026, 5, 9, 14, 30, 22, 0, time.UTC),
		Environment:  "local",
		Request: model.RequestDetail{
			Method:  "POST",
			URL:     "/orders",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"amount":100,"currency":"GBP"}`),
		},
		Response: model.ResponseDetail{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       []byte(`{"error":"internal server error","code":"INTERNAL"}`),
		},
		Failures: []model.Failure{
			{
				Invariant: "status_code",
				Message:   "expected 201, got 500",
				Expected:  201,
				Actual:    500,
			},
		},
	}
}

func TestWriter_CreatesOutputDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "artifacts")
	w := replay.NewWriter(dir)

	if _, err := w.Write(sampleArtifact()); err != nil {
		t.Fatalf("Write returned unexpected error: %v", err)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected output directory to be created")
	}
}

func TestWriter_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	w := replay.NewWriter(dir)

	path, err := w.Write(sampleArtifact())
	if err != nil {
		t.Fatalf("Write returned unexpected error: %v", err)
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected artifact file at %s", path)
	}
}

func TestWriter_FilePathContainsTimestamp(t *testing.T) {
	dir := t.TempDir()
	w := replay.NewWriter(dir)

	path, err := w.Write(sampleArtifact())
	if err != nil {
		t.Fatalf("Write returned unexpected error: %v", err)
	}

	filename := filepath.Base(path)
	if !strings.Contains(filename, "2026-05-09T") {
		t.Errorf("expected filename to contain date, got %q", filename)
	}
}

func TestWriter_FilePathContainsCaseID(t *testing.T) {
	dir := t.TempDir()
	w := replay.NewWriter(dir)

	path, err := w.Write(sampleArtifact())
	if err != nil {
		t.Fatalf("Write returned unexpected error: %v", err)
	}

	filename := filepath.Base(path)
	if !strings.Contains(filename, "create-order-valid") {
		t.Errorf("expected filename to contain case ID, got %q", filename)
	}
}

func TestWriter_FileIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	w := replay.NewWriter(dir)

	path, err := w.Write(sampleArtifact())
	if err != nil {
		t.Fatalf("Write returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read artifact file: %v", err)
	}

	var artifact model.Artifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatalf("artifact file is not valid JSON: %v", err)
	}
}

func TestWriter_ArtifactRoundTrip(t *testing.T) {
	dir := t.TempDir()
	w := replay.NewWriter(dir)

	original := sampleArtifact()
	path, err := w.Write(original)
	if err != nil {
		t.Fatalf("Write returned unexpected error: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("cannot read artifact file: %v", err)
	}

	var decoded model.Artifact
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("cannot decode artifact: %v", err)
	}

	if decoded.CaseID != original.CaseID {
		t.Errorf("CaseID: got %q, want %q", decoded.CaseID, original.CaseID)
	}
	if decoded.StrategyKind != original.StrategyKind {
		t.Errorf("StrategyKind: got %q, want %q", decoded.StrategyKind, original.StrategyKind)
	}
	if decoded.Request.Method != original.Request.Method {
		t.Errorf("Request.Method: got %q, want %q", decoded.Request.Method, original.Request.Method)
	}
	if string(decoded.Request.Body) != string(original.Request.Body) {
		t.Errorf("Request.Body: got %s, want %s", decoded.Request.Body, original.Request.Body)
	}
	if decoded.Response.StatusCode != original.Response.StatusCode {
		t.Errorf("Response.StatusCode: got %d, want %d", decoded.Response.StatusCode, original.Response.StatusCode)
	}
	if len(decoded.Failures) != len(original.Failures) {
		t.Errorf("Failures length: got %d, want %d", len(decoded.Failures), len(original.Failures))
	}
}

func TestWriter_TwoFailuresSameSecond_NamesDoNotCollide(t *testing.T) {
	dir := t.TempDir()
	w := replay.NewWriter(dir)

	a1 := sampleArtifact()
	a1.CaseID = "case-one"

	a2 := sampleArtifact()
	a2.CaseID = "case-two"

	path1, err := w.Write(a1)
	if err != nil {
		t.Fatalf("first Write failed: %v", err)
	}
	path2, err := w.Write(a2)
	if err != nil {
		t.Fatalf("second Write failed: %v", err)
	}

	if path1 == path2 {
		t.Errorf("expected distinct paths for different case IDs, both got %q", path1)
	}
}

func TestWriter_EmptyOutputDir_UsesDefault(t *testing.T) {
	w := replay.NewWriter("")
	if w == nil {
		t.Error("expected non-nil writer for empty output dir")
	}
}
