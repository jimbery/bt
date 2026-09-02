package fuzz_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jimbery/bt/internal/strategy/fuzz"
	"github.com/jimbery/bt/pkg/model"
)

func discardRunner(artifactDir string) *fuzz.Runner {
	log := slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return fuzz.NewRunnerWithLogger(log, artifactDir)
}

func safePlan(baseURL string, ops ...model.Operation) model.TestPlan {
	return model.TestPlan{
		Target:     model.Target{BaseURL: baseURL},
		Operations: ops,
		RunConfig: model.RunConfig{
			FuzzIterations: 10, // keep tests fast
			Safety: model.SafetyConfig{
				Profile: "safe",
			},
		},
	}
}

func getOp(id string) model.Operation {
	return model.Operation{
		ID:     id,
		Method: "GET",
		Path:   "/orders/ord-001",
		Responses: []model.ResponseSpec{{
			StatusCode: 200,
			Schema: &model.SchemaRef{
				Type:     "object",
				Required: []string{"id"},
				Properties: map[string]*model.SchemaRef{
					"id": {Type: "string"},
				},
			},
		}},
	}
}

// okJSONServer returns 200 + JSON for every path (path mutators must not turn a "all pass" run into 404s).
func okJSONServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
}

// --- Safety enforcement ---

func TestRunner_BlockedMethod_SkipsOperation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("blocked method should never reach the server")
	}))
	defer srv.Close()

	deleteOp := model.Operation{
		ID:     "DeleteOrder",
		Method: "DELETE",
		Path:   "/orders/ord-001",
	}
	plan := safePlan(srv.URL, deleteOp)

	runner := discardRunner(t.TempDir())
	results, err := runner.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for a skipped operation")
	}
	if !results[0].Skipped {
		t.Errorf("expected Skipped=true for DELETE under safe profile, got false")
	}
	if results[0].SkipReason == "" {
		t.Error("expected SkipReason to be set for skipped operation")
	}
}

func TestRunner_AllowedMethod_ReachesServer(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"ord-001"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := discardRunner(t.TempDir())
	runner.Run(context.Background(), plan)

	if !reached {
		t.Error("GET (allowed by safe profile) should have reached the server")
	}
}

// --- Classification in results ---

func TestRunner_CrashResponse_ClassifiedAsCrash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a crash: drop the connection.
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := discardRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "crash" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected at least one crash classification for a dropped connection")
	}
}

func TestRunner_ValidationLeak_ClassifiedCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"goroutine 1 [running]: main.handler panic"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := discardRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "validation_leak" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected validation_leak classification for body containing goroutine trace")
	}
}

func TestRunner_SchemaBreak_ClassifiedCorrectly(t *testing.T) {
	// Server returns body missing required 'id' field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`)) // missing required 'id'
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := discardRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "schema_break" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected schema_break classification for response missing required field")
	}
}

func TestRunner_UnexpectedStatus_ClassifiedCorrectly(t *testing.T) {
	// Server returns 418, which is not declared in the operation's response schemas.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(418)
		w.Write([]byte(`{"error":"I am a teapot"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := discardRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "unexpected_status" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected unexpected_status for 418 response from op declaring only 200")
	}
}

// --- Artifact production ---

func TestRunner_NonPassClassification_WritesArtifact(t *testing.T) {
	artifactDir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"always fails"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := discardRunner(artifactDir)
	runner.Run(context.Background(), plan)

	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		t.Fatalf("cannot read artifact dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one artifact for a non-pass classification")
	}
}

func TestRunner_PassClassification_NoArtifactWritten(t *testing.T) {
	artifactDir := t.TempDir()
	srv := okJSONServer(t, `{"id":"ord-001"}`)
	defer srv.Close()

	// Only run payload + header variants (MutateAll order); path mutator can create
	// huge segments that trigger 414/431 — undeclared statuses and replay artifacts.
	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{getOp("GetOrder")},
		RunConfig: model.RunConfig{
			FuzzIterations: 2,
			Safety: model.SafetyConfig{
				Profile:        "safe",
				AllowedMethods: []string{"GET"},
			},
		},
	}
	runner := discardRunner(artifactDir)
	runner.Run(context.Background(), plan)

	entries, _ := os.ReadDir(artifactDir)
	if len(entries) != 0 {
		t.Errorf("expected no artifacts for passing fuzz run, got %d", len(entries))
	}
}

// --- Result shape ---

func TestRunner_Results_HaveStrategyKindFuzz(t *testing.T) {
	srv := okJSONServer(t, `{"id":"ord-001"}`)
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := discardRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	for _, r := range results {
		if r.StrategyKind != "fuzz" {
			t.Errorf("expected StrategyKind='fuzz', got %q", r.StrategyKind)
		}
	}
}

func TestRunner_Failure_ClassificationFieldIsSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := discardRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "" {
				t.Error("every fuzz failure must have a Classification set")
			}
		}
	}
}

// --- Context cancellation ---

func TestRunner_ContextCancellation_StopsCleanly(t *testing.T) {
	srv := okJSONServer(t, `{"id":"ord-001"}`)
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{getOp("GetOrder")},
		RunConfig: model.RunConfig{
			FuzzIterations: 1000,
			Safety:         model.SafetyConfig{Profile: "safe"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := discardRunner(t.TempDir())
	_, err := runner.Run(ctx, plan)
	_ = err // should not hang
}

// --- Corpus saving ---

func TestRunner_InterestingInput_SavedToCorpus(t *testing.T) {
	corpusDir := t.TempDir()
	artifactDir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"always fails"}`))
	}))
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{getOp("GetOrder")},
		RunConfig: model.RunConfig{
			FuzzIterations: 5,
			CorpusDir:      corpusDir,
			Safety:         model.SafetyConfig{Profile: "safe"},
		},
	}

	runner := discardRunner(artifactDir)
	runner.Run(context.Background(), plan)

	entries, _ := os.ReadDir(corpusDir)
	if len(entries) == 0 {
		t.Error("expected at least one interesting input to be saved to corpus on failure")
	}
}
