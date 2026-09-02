package replay_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jimbery/bt/internal/replay"
	"github.com/jimbery/bt/pkg/model"
)

func writeArtifactFile(t *testing.T, dir string, a model.Artifact) string {
	t.Helper()
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "2026-05-09T143022Z-"+a.CaseID+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoader_LoadsArtifact(t *testing.T) {
	dir := t.TempDir()
	original := model.Artifact{
		ID:           "artifact-001",
		StrategyKind: "table",
		CaseID:       "create-order-valid",
		OccurredAt:   time.Now().UTC().Truncate(time.Second),
		Environment:  "local",
		Request: model.RequestDetail{
			Method: "POST",
			URL:    "/orders",
			Body:   []byte(`{"amount":100}`),
		},
		Response: model.ResponseDetail{
			StatusCode: 500,
		},
		Failures: []model.Failure{
			{Invariant: "status_code", Message: "expected 201, got 500"},
		},
	}

	path := writeArtifactFile(t, dir, original)

	l := replay.NewLoader()
	loaded, err := l.Load(path)
	if err != nil {
		t.Fatalf("Load returned unexpected error: %v", err)
	}

	if loaded.CaseID != original.CaseID {
		t.Errorf("CaseID: got %q, want %q", loaded.CaseID, original.CaseID)
	}
	if loaded.Response.StatusCode != original.Response.StatusCode {
		t.Errorf("Response.StatusCode: got %d, want %d", loaded.Response.StatusCode, original.Response.StatusCode)
	}
	if len(loaded.Failures) != 1 {
		t.Errorf("Failures length: got %d, want 1", len(loaded.Failures))
	}
}

func TestLoader_FileNotFound(t *testing.T) {
	l := replay.NewLoader()
	_, err := l.Load("/nonexistent/artifact.json")
	if !errors.Is(err, replay.ErrArtifactNotFound) {
		t.Errorf("expected ErrArtifactNotFound, got %v", err)
	}
}

func TestLoader_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}

	l := replay.NewLoader()
	_, err := l.Load(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if errors.Is(err, replay.ErrArtifactNotFound) {
		t.Error("should not return ErrArtifactNotFound for invalid JSON")
	}
}

func TestLoader_ListArtifacts_ReturnsSortedNewestFirst(t *testing.T) {
	dir := t.TempDir()

	older := model.Artifact{CaseID: "older-case"}
	newer := model.Artifact{CaseID: "newer-case"}

	data1, _ := json.MarshalIndent(older, "", "  ")
	data2, _ := json.MarshalIndent(newer, "", "  ")

	if err := os.WriteFile(filepath.Join(dir, "2026-05-09T100000Z-older-case.json"), data1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-05-09T120000Z-newer-case.json"), data2, 0o644); err != nil {
		t.Fatal(err)
	}

	l := replay.NewLoader()
	paths, err := l.List(dir)
	if err != nil {
		t.Fatalf("List returned unexpected error: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(paths))
	}

	if !contains(paths[0], "newer-case") {
		t.Errorf("expected newest artifact first, got %q", paths[0])
	}
}

func TestLoader_ListArtifacts_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	l := replay.NewLoader()

	paths, err := l.List(dir)
	if err != nil {
		t.Fatalf("List returned unexpected error: %v", err)
	}
	if len(paths) != 0 {
		t.Errorf("expected empty list for empty directory, got %d entries", len(paths))
	}
}

func TestLoader_ListArtifacts_IgnoresNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "not-an-artifact.txt"), []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2026-05-09T100000Z-case.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	l := replay.NewLoader()
	paths, err := l.List(dir)
	if err != nil {
		t.Fatalf("List returned unexpected error: %v", err)
	}
	if len(paths) != 1 {
		t.Errorf("expected 1 artifact, got %d", len(paths))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			containsAt(s, substr)))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
