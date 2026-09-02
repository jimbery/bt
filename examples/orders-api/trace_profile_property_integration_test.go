//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	openapiadapt "github.com/jimbery/bt/internal/adapter/openapi"
	"github.com/jimbery/bt/internal/replay"
	"github.com/jimbery/bt/internal/runner"
	"github.com/jimbery/bt/internal/strategy"
	"github.com/jimbery/bt/internal/strategy/property"
	"github.com/jimbery/bt/internal/testutil"
	"github.com/jimbery/bt/internal/trace/analyze"
	"github.com/jimbery/bt/internal/trace/har"
	"github.com/jimbery/bt/pkg/model"
)

type recordBodies struct {
	inner strategy.Executor
	mu    sync.Mutex
	raw   [][]byte
}

func (r *recordBodies) Run(ctx context.Context, in model.CaseInput) (model.ResponseDetail, error) {
	body := caseInputBodyBytes(in)
	r.mu.Lock()
	r.raw = append(r.raw, body)
	r.mu.Unlock()
	return r.inner.Run(ctx, in)
}

func caseInputBodyBytes(in model.CaseInput) []byte {
	switch v := in.Body.(type) {
	case nil:
		return nil
	case []byte:
		return append([]byte(nil), v...)
	case string:
		return []byte(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return b
	}
}

func (r *recordBodies) currencies() map[string]int {
	out := map[string]int{}
	for _, b := range r.raw {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			continue
		}
		c, _ := m["currency"].(string)
		out[c]++
	}
	return out
}

func loadSampleTraceProfile(t *testing.T) *model.TraceProfile {
	t.Helper()
	root := testutil.RepoRoot(t)
	schema := filepath.Join(root, "examples/orders-api/spec/openapi.yaml")
	harPath := filepath.Join(root, "examples/orders-api/bt/trace/sample.har")
	data, err := os.ReadFile(harPath)
	if err != nil {
		t.Fatalf("read har: %v", err)
	}
	h, err := har.Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse har: %v", err)
	}
	ad := openapiadapt.New()
	ops, err := ad.Discover(context.Background(), model.Target{SchemaPath: schema})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	prof, err := analyze.Analyze(h.ToEntries(), ops, "sample.har")
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}
	return prof
}

func TestProperty_CreateOrder_UsesTraceCurrencyDistribution(t *testing.T) {
	root := testutil.RepoRoot(t)
	schema := filepath.Join(root, "examples/orders-api/spec/openapi.yaml")
	ad := openapiadapt.New()
	ops, err := ad.Discover(context.Background(), model.Target{SchemaPath: schema})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	prof := loadSampleTraceProfile(t)
	srv := newTestServer(t)
	defer srv.Close()

	httpExec := runner.New(runner.Config{BaseURL: srv.URL})
	rec := &recordBodies{inner: httpExec}

	st := property.NewWithOptions(property.Options{
		ArtifactWriter: replay.NewWriter(filepath.Join(t.TempDir(), "artifacts")),
		TraceProfile:   prof,
	})
	spec := strategy.Spec{
		Kind:       strategy.KindProperty,
		Operations: []string{"CreateOrder"},
		Invariants: []model.Invariant{
			{Name: model.InvariantNo5xx},
			{Name: model.InvariantResponseMatchesSchema},
		},
		Config: map[string]any{"checks": 500, "seed": int64(2026)},
	}
	cases, err := st.Plan(context.Background(), spec, ops)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	results, err := st.Execute(context.Background(), cases, rec)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, r := range results {
		if !r.Passed {
			t.Fatalf("unexpected failure: %+v", r.Failures)
		}
	}

	counts := rec.currencies()
	n := counts["GBP"] + counts["USD"] + counts["EUR"]
	if n < 400 {
		t.Fatalf("expected mostly known currencies in bodies, counts=%v", counts)
	}
	if frac := float64(counts["GBP"]) / float64(n); frac < 0.60 {
		t.Fatalf("GBP fraction want >= 0.60, got %.3f (counts=%v)", frac, counts)
	}
}
