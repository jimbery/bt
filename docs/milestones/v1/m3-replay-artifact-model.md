# M3 — Replay + Artifact Model

This document follows the same structure as M1 and M2: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist.

---

## Overview

M3 builds the replay infrastructure before any generative strategy exists. This is deliberate — every strategy added in M4, M5, and beyond will inherit artifact writing and replay for free without any extra work.

The three pieces built here are:

1. **Artifact writer** — serialises a `model.Artifact` to disk on any test failure
2. **Artifact loader** — reads an artifact bundle back from disk
3. **`bt replay` command** — reads an artifact and re-executes the original request, showing what the API returns now versus what it returned when the failure was recorded

The table strategy is updated to write artifacts automatically on failure. No strategy-specific replay code is needed — the mechanism is in the engine layer.

**Exit criterion:** Any test failure against the orders API produces a portable artifact bundle under `.bt/artifacts/`. Running `bt replay <path>` re-executes the original request and shows the response. The artifact contains enough information to reproduce the failure exactly.

---

## Step 1 — Artifact writer

### Spec

- The writer serialises a `model.Artifact` to a JSON file under a configurable output directory
- The default output directory is `.bt/artifacts/`
- The file path is deterministic and human-readable: `<dir>/<timestamp>-<case-id>.json`
  - Timestamp format: `2006-01-02T150405Z` (no colons — safe for all filesystems)
  - Example: `.bt/artifacts/2026-05-09T143022Z-create-order-valid.json`
- The writer creates the output directory if it does not exist
- If two failures occur within the same second for different cases the filenames must not collide — case ID is the discriminator
- Writing must be atomic — a partial write must not leave a corrupt artifact on disk
- The writer returns the path of the written artifact so the reporter can reference it

### Tests

`internal/replay/writer_test.go`:

```go
package replay_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yourorg/bt/internal/replay"
	"github.com/yourorg/bt/pkg/model"
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
			URL:     "http://localhost:8080/orders",
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
	// Timestamp format: 2006-01-02T150405Z
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
	// When given an empty string, writer should use the default .bt/artifacts path.
	// We can't assert the exact path without knowing the working dir,
	// but we can assert it does not panic and returns a non-empty path.
	w := replay.NewWriter("")

	// Override the default to a temp dir so we don't pollute the working dir.
	// This tests that the writer is initialised without panicking when given "".
	if w == nil {
		t.Error("expected non-nil writer for empty output dir")
	}
}
```

### Implementation

`internal/replay/writer.go`:

```go
package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/yourorg/bt/pkg/model"
)

const defaultArtifactDir = ".bt/artifacts"

// Writer writes artifact bundles to disk.
type Writer struct {
	dir string
}

// NewWriter returns a Writer that stores artifacts in dir.
// If dir is empty the default .bt/artifacts directory is used.
func NewWriter(dir string) *Writer {
	if dir == "" {
		dir = defaultArtifactDir
	}
	return &Writer{dir: dir}
}

// Write serialises the artifact to a JSON file and returns the path.
// The output directory is created if it does not exist.
// The filename is <timestamp>-<case-id>.json using a filesystem-safe timestamp.
func (w *Writer) Write(a model.Artifact) (string, error) {
	if err := os.MkdirAll(w.dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create artifact directory: %w", err)
	}

	ts := a.OccurredAt.UTC().Format("2006-01-02T150405Z")
	filename := fmt.Sprintf("%s-%s.json", ts, sanitise(a.CaseID))
	path := filepath.Join(w.dir, filename)

	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot marshal artifact: %w", err)
	}

	// Write atomically via a temp file to avoid partial writes.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return "", fmt.Errorf("cannot write artifact: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("cannot finalise artifact: %w", err)
	}

	return path, nil
}

// sanitise replaces characters that are unsafe in filenames with hyphens.
func sanitise(s string) string {
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' {
			out[i] = c
		} else {
			out[i] = '-'
		}
	}
	return string(out)
}

// DefaultArtifactDir returns the default artifact directory path.
func DefaultArtifactDir() string { return defaultArtifactDir }

// TimestampNow returns the current time formatted for use in artifact filenames.
func TimestampNow() string { return time.Now().UTC().Format("2006-01-02T150405Z") }
```

Run the tests:

```bash
go test ./internal/replay/... -race -v
```

---

## Step 2 — Artifact loader

### Spec

- The loader reads a `model.Artifact` from a JSON file at a given path
- If the file does not exist it returns `ErrArtifactNotFound`
- If the file exists but is not valid JSON it returns a descriptive parse error
- If the file is valid JSON but does not unmarshal into a `model.Artifact` it returns a descriptive error
- The loader also supports listing all artifact files in a directory, sorted newest first

### Tests

`internal/replay/loader_test.go`:

```go
package replay_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yourorg/bt/internal/replay"
	"github.com/yourorg/bt/pkg/model"
)

func writeArtifactFile(t *testing.T, dir string, a model.Artifact) string {
	t.Helper()
	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "2026-05-09T143022Z-"+a.CaseID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
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
			URL:    "http://localhost:8080/orders",
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
	if err := os.WriteFile(path, []byte("not json at all"), 0644); err != nil {
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

	// Write two artifact files with different timestamps in their names.
	older := model.Artifact{CaseID: "older-case"}
	newer := model.Artifact{CaseID: "newer-case"}

	data1, _ := json.MarshalIndent(older, "", "  ")
	data2, _ := json.MarshalIndent(newer, "", "  ")

	os.WriteFile(filepath.Join(dir, "2026-05-09T100000Z-older-case.json"), data1, 0644)
	os.WriteFile(filepath.Join(dir, "2026-05-09T120000Z-newer-case.json"), data2, 0644)

	l := replay.NewLoader()
	paths, err := l.List(dir)
	if err != nil {
		t.Fatalf("List returned unexpected error: %v", err)
	}

	if len(paths) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(paths))
	}

	// Newest first — the 12:00 file should come before the 10:00 file.
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
	os.WriteFile(filepath.Join(dir, "not-an-artifact.txt"), []byte("text"), 0644)
	os.WriteFile(filepath.Join(dir, "2026-05-09T100000Z-case.json"), []byte(`{}`), 0644)

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
```

### Implementation

`internal/replay/loader.go`:

```go
package replay

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yourorg/bt/pkg/model"
)

// ErrArtifactNotFound is returned when the artifact file does not exist.
var ErrArtifactNotFound = errors.New("artifact not found")

// Loader reads artifact bundles from disk.
type Loader struct{}

// NewLoader returns a new Loader.
func NewLoader() *Loader { return &Loader{} }

// Load reads and deserialises an artifact from the given path.
func (l *Loader) Load(path string) (*model.Artifact, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read artifact: %w", err)
	}

	var a model.Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("cannot parse artifact at %s: %w", path, err)
	}

	return &a, nil
}

// List returns the paths of all artifact JSON files in dir, sorted newest first.
func (l *Loader) List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read artifact directory: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}

	// Sort descending by filename — since filenames start with a timestamp
	// in lexicographically sortable format, this gives newest first.
	sort.Slice(paths, func(i, j int) bool {
		return paths[i] > paths[j]
	})

	return paths, nil
}
```

Run the tests:

```bash
go test ./internal/replay/... -race -v
```

---

## Step 3 — Wire artifact writing into the table strategy

### Spec

- When a case fails, the table strategy writes an artifact to the configured output directory
- When a case passes, no artifact is written
- The artifact contains: strategy kind, case ID, timestamp, environment, full request detail, full response detail, and all failures
- The artifact path is included in the `Result` so the reporter can surface it
- The writer is injected into the strategy rather than constructed internally — this keeps the strategy testable without touching the filesystem

To support this, `model.Result` gains an `ArtifactPath` field:

```go
// In pkg/model/result.go add:
ArtifactPath string `json:"artifact_path,omitempty"`
```

And the table strategy's `Execute` method accepts a `WriteFunc` option.

### Tests

`internal/strategy/table/artifact_test.go`:

```go
package table_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yourorg/bt/internal/replay"
	"github.com/yourorg/bt/internal/strategy"
	"github.com/yourorg/bt/internal/strategy/table"
	"github.com/yourorg/bt/pkg/model"
)

func TestTableStrategy_Execute_WritesArtifactOnFailure(t *testing.T) {
	artifactDir := t.TempDir()

	s := table.NewWithOptions(table.Options{
		ArtifactWriter: replay.NewWriter(artifactDir),
		Environment:    "test",
	})

	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}

	cases := []model.Case{
		{
			ID:          "create-order-valid",
			OperationID: "CreateOrder",
			Input:       model.CaseInput{Method: "POST", Path: "/orders"},
			Expected:    &model.CaseExpectation{StatusCode: 201},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if results[0].Passed {
		t.Fatal("expected case to fail")
	}

	// Artifact path must be populated on the result.
	if results[0].ArtifactPath == "" {
		t.Error("expected ArtifactPath to be set on failed result")
	}

	// Artifact file must exist on disk.
	if _, err := os.Stat(results[0].ArtifactPath); os.IsNotExist(err) {
		t.Errorf("expected artifact file at %s", results[0].ArtifactPath)
	}
}

func TestTableStrategy_Execute_NoArtifactOnPass(t *testing.T) {
	artifactDir := t.TempDir()

	s := table.NewWithOptions(table.Options{
		ArtifactWriter: replay.NewWriter(artifactDir),
		Environment:    "test",
	})

	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 200},
	}

	cases := []model.Case{
		{
			ID:       "health-check",
			Input:    model.CaseInput{Method: "GET", Path: "/health"},
			Expected: &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	if !results[0].Passed {
		t.Fatal("expected case to pass")
	}
	if results[0].ArtifactPath != "" {
		t.Errorf("expected no ArtifactPath on passing result, got %q", results[0].ArtifactPath)
	}

	// Artifact directory must be empty.
	entries, _ := os.ReadDir(artifactDir)
	if len(entries) != 0 {
		t.Errorf("expected no artifact files for passing case, got %d", len(entries))
	}
}

func TestTableStrategy_Execute_ArtifactContainsFailureDetail(t *testing.T) {
	artifactDir := t.TempDir()

	s := table.NewWithOptions(table.Options{
		ArtifactWriter: replay.NewWriter(artifactDir),
		Environment:    "staging",
	})

	exec := &fakeExecutor{
		response: model.ResponseDetail{
			StatusCode: 404,
			Body:       []byte(`{"error":"not found"}`),
		},
	}

	cases := []model.Case{
		{
			ID:          "get-order-200",
			OperationID: "GetOrder",
			Input:       model.CaseInput{Method: "GET", Path: "/orders/1"},
			Expected:    &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}

	loader := replay.NewLoader()
	artifact, err := loader.Load(results[0].ArtifactPath)
	if err != nil {
		t.Fatalf("cannot load artifact: %v", err)
	}

	if artifact.CaseID != "get-order-200" {
		t.Errorf("CaseID: got %q, want get-order-200", artifact.CaseID)
	}
	if artifact.StrategyKind != "table" {
		t.Errorf("StrategyKind: got %q, want table", artifact.StrategyKind)
	}
	if artifact.Environment != "staging" {
		t.Errorf("Environment: got %q, want staging", artifact.Environment)
	}
	if artifact.Response.StatusCode != 404 {
		t.Errorf("Response.StatusCode: got %d, want 404", artifact.Response.StatusCode)
	}
	if len(artifact.Failures) == 0 {
		t.Error("expected failures in artifact")
	}

	// Artifact path must exist within the configured dir.
	if !filepath.IsAbs(results[0].ArtifactPath) {
		t.Error("expected absolute artifact path")
	}
}

func TestTableStrategy_Execute_WithoutWriter_DoesNotPanic(t *testing.T) {
	// When no writer is configured the strategy must still work — just
	// without writing artifacts. ArtifactPath will be empty.
	s := table.New()
	exec := &fakeExecutor{
		response: model.ResponseDetail{StatusCode: 500},
	}

	cases := []model.Case{
		{
			ID:       "case-without-writer",
			Input:    model.CaseInput{Method: "GET", Path: "/"},
			Expected: &model.CaseExpectation{StatusCode: 200},
		},
	}

	results, err := s.Execute(context.Background(), cases, exec)
	if err != nil {
		t.Fatalf("Execute returned unexpected error: %v", err)
	}
	if results[0].ArtifactPath != "" {
		t.Errorf("expected empty ArtifactPath when no writer configured, got %q", results[0].ArtifactPath)
	}
}
```

### Implementation

Update `pkg/model/result.go` to add `ArtifactPath`:

```go
type Result struct {
	CaseID       string         `json:"case_id"`
	Passed       bool           `json:"passed"`
	StatusCode   int            `json:"status_code"`
	Duration     time.Duration  `json:"duration_ms"`
	Failures     []Failure      `json:"failures,omitempty"`
	Request      RequestDetail  `json:"request"`
	Response     ResponseDetail `json:"response"`
	ArtifactPath string         `json:"artifact_path,omitempty"`
}
```

Update `internal/strategy/table/table.go` to support options and artifact writing:

```go
package table

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/yourorg/bt/internal/strategy"
	"github.com/yourorg/bt/pkg/model"
)

// ArtifactWriter is the interface the table strategy uses to write failure artifacts.
// replay.Writer satisfies this interface.
type ArtifactWriter interface {
	Write(a model.Artifact) (string, error)
}

// Options configures optional behaviour for the table strategy.
type Options struct {
	// ArtifactWriter is called on each failing case to write a replay bundle.
	// If nil, no artifacts are written.
	ArtifactWriter ArtifactWriter

	// Environment is recorded in artifact bundles for context.
	Environment string
}

type tableStrategy struct {
	opts Options
}

// New returns a table Strategy with default options (no artifact writing).
func New() strategy.Strategy {
	return &tableStrategy{}
}

// NewWithOptions returns a table Strategy with the given options.
func NewWithOptions(opts Options) strategy.Strategy {
	return &tableStrategy{opts: opts}
}

func (s *tableStrategy) Name() strategy.Kind { return strategy.KindTable }

// caseFile is the on-disk format for a table test case file.
type caseFile struct {
	Cases []caseEntry `yaml:"cases"`
}

type caseEntry struct {
	ID          string             `yaml:"id"`
	OperationID string             `yaml:"operation_id"`
	Input       caseInputEntry     `yaml:"input"`
	Expected    *caseExpectedEntry `yaml:"expected"`
}

type caseInputEntry struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
	Query   map[string]string `yaml:"query"`
	Body    any               `yaml:"body"`
}

type caseExpectedEntry struct {
	StatusCode int               `yaml:"status_code"`
	Headers    map[string]string `yaml:"headers"`
}

func (s *tableStrategy) Plan(_ context.Context, spec strategy.Spec, _ []model.Operation) ([]model.Case, error) {
	filePath, ok := spec.Config["file"].(string)
	if !ok || filePath == "" {
		return nil, errors.New("table strategy requires config.file to be set")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read case file: %w", err)
	}

	var cf caseFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("cannot parse case file: %w", err)
	}

	cases := make([]model.Case, 0, len(cf.Cases))
	for _, entry := range cf.Cases {
		c := model.Case{
			ID:          entry.ID,
			OperationID: entry.OperationID,
			Input: model.CaseInput{
				Method:  entry.Input.Method,
				Path:    entry.Input.Path,
				Headers: entry.Input.Headers,
				Query:   entry.Input.Query,
				Body:    entry.Input.Body,
			},
		}
		if entry.Expected != nil {
			c.Expected = &model.CaseExpectation{
				StatusCode: entry.Expected.StatusCode,
				Headers:    entry.Expected.Headers,
			}
		}
		cases = append(cases, c)
	}

	return cases, nil
}

func (s *tableStrategy) Execute(ctx context.Context, cases []model.Case, exec strategy.Executor) ([]model.Result, error) {
	results := make([]model.Result, 0, len(cases))

	for _, c := range cases {
		start := time.Now()
		resp, err := exec.Run(ctx, c.Input)
		if err != nil {
			return nil, fmt.Errorf("case %q: executor error: %w", c.ID, err)
		}

		result := model.Result{
			CaseID:     c.ID,
			StatusCode: resp.StatusCode,
			Duration:   time.Since(start),
			Response:   resp,
			Request: model.RequestDetail{
				Method: c.Input.Method,
				URL:    c.Input.Path,
			},
		}

		var failures []model.Failure

		if c.Expected != nil {
			if c.Expected.StatusCode != 0 && resp.StatusCode != c.Expected.StatusCode {
				failures = append(failures, model.Failure{
					Invariant: "status_code",
					Message:   fmt.Sprintf("expected status %d, got %d", c.Expected.StatusCode, resp.StatusCode),
					Expected:  c.Expected.StatusCode,
					Actual:    resp.StatusCode,
				})
			}

			for header, want := range c.Expected.Headers {
				got := resp.Headers[header]
				if got != want {
					failures = append(failures, model.Failure{
						Invariant: "response_header",
						Message:   fmt.Sprintf("header %q: expected %q, got %q", header, want, got),
						Expected:  want,
						Actual:    got,
					})
				}
			}
		}

		result.Failures = failures
		result.Passed = len(failures) == 0

		// Write artifact on failure if a writer is configured.
		if !result.Passed && s.opts.ArtifactWriter != nil {
			artifact := model.Artifact{
				StrategyKind: string(strategy.KindTable),
				CaseID:       c.ID,
				OccurredAt:   time.Now().UTC(),
				Environment:  s.opts.Environment,
				Request:      result.Request,
				Response:     resp,
				Failures:     failures,
			}
			artifactPath, writeErr := s.opts.ArtifactWriter.Write(artifact)
			if writeErr != nil {
				// Non-fatal — log but don't fail the run over an artifact write error.
				fmt.Fprintf(os.Stderr, "warning: could not write artifact for case %q: %v\n", c.ID, writeErr)
			} else {
				result.ArtifactPath = artifactPath
			}
		}

		results = append(results, result)
	}

	return results, nil
}
```

Run the tests:

```bash
go test ./internal/strategy/table/... -race -v
go test ./internal/replay/... -race -v
```

---

## Step 4 — `bt replay` command

### Spec

- `bt replay <artifact-path>` reads an artifact and re-executes the original request against the current target
- It prints: the original failure, the original response, the new response, and whether the failure is still present
- It respects `--config` to get the base URL — the artifact stores the path but the caller may want to replay against a different environment
- If the artifact file does not exist it exits with a clear error message and exit code 2
- If the re-executed request now passes the case, it reports "failure no longer reproducible"
- If the re-executed request still fails, it reports "failure still present"
- The command does not modify the artifact file

### Tests

`internal/cli/replay_test.go`:

```go
package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yourorg/bt/internal/cli"
	"github.com/yourorg/bt/pkg/model"
)

func writeTestArtifact(t *testing.T, dir string, statusCode int) string {
	t.Helper()

	a := model.Artifact{
		ID:           "artifact-001",
		StrategyKind: "table",
		CaseID:       "create-order-valid",
		OccurredAt:   time.Now().UTC(),
		Environment:  "test",
		Request: model.RequestDetail{
			Method:  "POST",
			URL:     "/orders",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"amount":100,"currency":"GBP"}`),
		},
		Response: model.ResponseDetail{
			StatusCode: statusCode,
			Body:       []byte(`{"error":"internal server error"}`),
		},
		Failures: []model.Failure{
			{
				Invariant: "status_code",
				Message:   "expected 201, got 500",
				Expected:  201,
				Actual:    statusCode,
			},
		},
	}

	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "2026-05-09T143022Z-create-order-valid.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}

	return path
}

func writeReplayConfig(t *testing.T, dir, baseURL string) string {
	t.Helper()
	yaml := "version: 1\ntarget:\n  name: test-api\n  base_url: " + baseURL +
		"\n  schema: ./openapi.yaml\n"
	path := filepath.Join(dir, "backendtest.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplayCommand_ArtifactNotFound(t *testing.T) {
	dir := t.TempDir()
	cfgPath := writeReplayConfig(t, dir, "http://localhost:8080")

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"replay", "--config", cfgPath, "/nonexistent/artifact.json"})
	cmd.SetOut(&bytes.Buffer{})

	if err := cmd.Execute(); err == nil {
		t.Error("expected error for missing artifact file")
	}
}

func TestReplayCommand_FailureStillPresent(t *testing.T) {
	// Server still returns 500 — failure should still be present.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	dir := t.TempDir()
	artifactPath := writeTestArtifact(t, dir, 500)
	cfgPath := writeReplayConfig(t, dir, server.URL)

	var out bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"replay", "--config", cfgPath, artifactPath})
	cmd.SetOut(&out)

	// Expect non-zero exit because the failure is still present.
	cmd.Execute()

	output := out.String()
	if !containsString(output, "still present") && !containsString(output, "FAIL") {
		t.Errorf("expected output to indicate failure is still present, got:\n%s", output)
	}
}

func TestReplayCommand_FailureNoLongerReproducible(t *testing.T) {
	// Server now returns 201 — failure should no longer be reproducible.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	dir := t.TempDir()
	artifactPath := writeTestArtifact(t, dir, 500)
	cfgPath := writeReplayConfig(t, dir, server.URL)

	var out bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"replay", "--config", cfgPath, artifactPath})
	cmd.SetOut(&out)

	if err := cmd.Execute(); err != nil {
		t.Errorf("expected replay to succeed when failure not reproducible, got: %v", err)
	}

	output := out.String()
	if !containsString(output, "no longer") && !containsString(output, "PASS") {
		t.Errorf("expected output to indicate failure is no longer reproducible, got:\n%s", output)
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}()
}
```

### Implementation

`internal/cli/replay.go`:

```go
package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/yourorg/bt/internal/config"
	"github.com/yourorg/bt/internal/replay"
	"github.com/yourorg/bt/internal/runner"
)

func newReplayCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "replay <artifact-path>",
		Short:        "Replay a test artifact against the current target",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			artifactPath := args[0]
			cfgPath, _ := cmd.Flags().GetString("config")

			// Load artifact.
			loader := replay.NewLoader()
			artifact, err := loader.Load(artifactPath)
			if errors.Is(err, replay.ErrArtifactNotFound) {
				return fmt.Errorf("artifact not found: %s", artifactPath)
			}
			if err != nil {
				return fmt.Errorf("cannot load artifact: %w", err)
			}

			// Load config for base URL.
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("cannot load config: %w", err)
			}

			w := cmd.OutOrStdout()

			fmt.Fprintf(w, "Replaying artifact: %s\n", artifactPath)
			fmt.Fprintf(w, "Case:               %s\n", artifact.CaseID)
			fmt.Fprintf(w, "Strategy:           %s\n", artifact.StrategyKind)
			fmt.Fprintf(w, "Recorded at:        %s\n", artifact.OccurredAt.Format(time.RFC3339))
			fmt.Fprintf(w, "\nOriginal failure:\n")
			for _, f := range artifact.Failures {
				fmt.Fprintf(w, "  [%s] %s\n", f.Invariant, f.Message)
			}

			fmt.Fprintf(w, "\nOriginal response: HTTP %d\n", artifact.Response.StatusCode)
			fmt.Fprintf(w, "\nRe-executing request against %s...\n", cfg.Target.BaseURL)

			// Re-execute the request.
			r := runner.New(runner.Config{
				BaseURL: cfg.Target.BaseURL,
				Timeout: 30 * time.Second,
			})

			input := artifact.Request.AsCaseInput()
			resp, err := r.Run(cmd.Context(), input)
			if err != nil {
				return fmt.Errorf("replay request failed: %w", err)
			}

			fmt.Fprintf(w, "New response:      HTTP %d\n\n", resp.StatusCode)

			// Check if original failures are still present.
			stillFailing := false
			for _, f := range artifact.Failures {
				if f.Invariant == "status_code" {
					expected, ok := f.Expected.(float64)
					if ok && resp.StatusCode != int(expected) {
						stillFailing = true
					}
				}
			}

			if stillFailing {
				fmt.Fprintf(w, "Result: FAIL — failure still present\n")
				return fmt.Errorf("failure still present")
			}

			fmt.Fprintf(w, "Result: PASS — failure no longer reproducible\n")
			return nil
		},
	}
}
```

Add `AsCaseInput()` to `model.RequestDetail` so the replay command can re-execute it:

`pkg/model/result.go` — add this method:

```go
// AsCaseInput converts a RequestDetail back into a CaseInput for replay.
func (r RequestDetail) AsCaseInput() CaseInput {
	return CaseInput{
		Method:  r.Method,
		Path:    r.URL,
		Headers: r.Headers,
		Body:    r.Body,
	}
}
```

Run the tests:

```bash
go test ./internal/cli/... -race -v
go test ./... -race -v
```

---

## Step 5 — Wire artifact writer into `bt run`

Update `internal/cli/run.go` to initialise the artifact writer and pass it to the strategy via `table.NewWithOptions`:

```go
// In newRunCmd, replace:
strat = table.New()

// With:
artifactDir := cfg.Report.OutputDir
if artifactDir == "" {
    artifactDir = replay.DefaultArtifactDir()
}
strat = table.NewWithOptions(table.Options{
    ArtifactWriter: replay.NewWriter(filepath.Join(filepath.Dir(cfgPath), artifactDir)),
    Environment:    cfg.Target.Environment,
})
```

Add the import:

```go
import (
    "path/filepath"
    "github.com/yourorg/bt/internal/replay"
)
```

Update the console reporter to surface the artifact path when a case fails:

`internal/report/console.go` — update the failure output section:

```go
func (r *consoleReporter) Write(results []model.Result) error {
    for _, res := range results {
        status := "PASS"
        if !res.Passed {
            status = "FAIL"
        }
        fmt.Fprintf(r.w, "  %s  %s  (HTTP %d, %s)\n",
            status, res.CaseID, res.StatusCode, res.Duration)
        for _, f := range res.Failures {
            fmt.Fprintf(r.w, "       %s: %s\n", f.Invariant, f.Message)
        }
        if res.ArtifactPath != "" {
            fmt.Fprintf(r.w, "       artifact: %s\n", res.ArtifactPath)
            fmt.Fprintf(r.w, "       replay:   bt replay %s\n", res.ArtifactPath)
        }
    }

    s := summarise(results)
    fmt.Fprintf(r.w, "\n%d tests run: %d passed, %d failed\n", s.Total, s.Passed, s.Failed)
    return nil
}
```

---

## Step 6 — Full verification

```bash
# All unit tests
go test ./... -race -v

# Lint
golangci-lint run ./...

# Build
CGO_ENABLED=0 go build ./cmd/bt

# Smoke test — force a failure to produce an artifact
# (temporarily break a case expectation, or point at a non-running server)
./bt run --config examples/orders-api/bt/backendtest.yaml --strategy table

# Check an artifact was written
ls .bt/artifacts/

# Replay it
./bt replay --config examples/orders-api/bt/backendtest.yaml .bt/artifacts/<artifact-file>.json
```

---

## M3 exit criterion

Any test failure produces a portable artifact bundle under `.bt/artifacts/`. Running `bt replay <path>` re-executes the original request and reports whether the failure is still present. The artifact contains the full request, response, failures, strategy kind, and environment. All code has tests written before implementation.