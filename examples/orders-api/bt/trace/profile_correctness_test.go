//go:build integration

package trace_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	openapiadapt "github.com/jayimbery/bt/internal/adapter/openapi"
	"github.com/jayimbery/bt/internal/testutil"
	"github.com/jayimbery/bt/internal/trace/analyze"
	"github.com/jayimbery/bt/internal/trace/har"
	"github.com/jayimbery/bt/pkg/model"
)

func TestTraceProfileFromSampleHAR_OperationCountsAndCurrency(t *testing.T) {
	root := testutil.RepoRoot(t)
	harPath := filepath.Join(root, "examples/orders-api/bt/trace/sample.har")
	schemaPath := filepath.Join(root, "examples/orders-api/spec/openapi.yaml")

	f, err := os.Open(harPath)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer func() { _ = f.Close() }()
	h, err := har.Parse(f)
	if err != nil {
		t.Fatalf("parse har: %v", err)
	}

	ad := openapiadapt.New()
	ops, err := ad.Discover(context.Background(), model.Target{SchemaPath: schemaPath})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	prof, err := analyze.Analyze(h.ToEntries(), ops, filepath.Base(harPath))
	if err != nil {
		t.Fatalf("analyze: %v", err)
	}

	co := prof.Operations["CreateOrder"]
	if co == nil || co.CallCount < 90 {
		t.Fatalf("CreateOrder: want call_count >= 90, got %#v", co)
	}
	goOp := prof.Operations["GetOrder"]
	if goOp == nil || goOp.CallCount < 80 {
		t.Fatalf("GetOrder: want call_count >= 80, got %#v", goOp)
	}
	lo := prof.Operations["ListOrders"]
	if lo == nil || lo.CallCount < 20 {
		t.Fatalf("ListOrders: want call_count >= 20, got %#v", lo)
	}

	cur := co.Arguments["currency"]
	if cur == nil || len(cur.Distribution) == 0 {
		t.Fatalf("CreateOrder.currency distribution missing: %#v", cur)
	}
	if g := cur.Distribution["GBP"]; g < 0.65 || g > 0.75 {
		t.Errorf("GBP share want ~0.70 (band 0.65–0.75), got %v", cur.Distribution)
	}

	if prof.Sequences == nil || prof.Sequences.Transitions == nil {
		t.Fatal("expected sequences with transitions")
	}
	row := prof.Sequences.Transitions["CreateOrder"]
	if row == nil {
		t.Fatal("missing transitions row for CreateOrder")
	}
	if p := row["GetOrder"]; p <= 0.70 {
		t.Errorf("CreateOrder→GetOrder transition want > 0.70, got %v (row=%v)", p, row)
	}
}
