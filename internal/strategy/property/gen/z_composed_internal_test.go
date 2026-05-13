package gen

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/jayimbery/bt/pkg/model"
)

func TestFilterDistributionNonEmpty(t *testing.T) {
	schema := &model.SchemaRef{Type: "string"}
	d := map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05}
	v := filterDistribution(schema, d)
	if len(v) == 0 {
		t.Fatalf("expected non-nil distribution map")
	}
}

func TestTraceDistributionChoicesMarginal(t *testing.T) {
	ch := TraceDistributionChoices(map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05})
	if len(ch) != 1000 {
		t.Fatalf("want 1000 slots, got %d", len(ch))
	}
	gbp := 0
	for _, s := range ch {
		if s == "GBP" {
			gbp++
		}
	}
	if gbp != 700 {
		t.Fatalf("want 700 GBP slots, got %d", gbp)
	}
}

// Uniform index over the multiset recovers the intended marginal (verified without Rapid,
// which biases integer draws for shrinking).
func TestTraceDistributionChoicesHistogramRand(t *testing.T) {
	ch := TraceDistributionChoices(map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05})
	r := rand.New(rand.NewPCG(11, 22))
	const n = 200_000
	gbp := 0
	for i := 0; i < n; i++ {
		if ch[r.IntN(len(ch))] == "GBP" {
			gbp++
		}
	}
	ratio := float64(gbp) / n
	if ratio < 0.68 || ratio > 0.72 || math.IsNaN(ratio) {
		t.Fatalf("GBP ratio want ~0.70, got %f", ratio)
	}
}

func TestBuildComposedUsesDistributionWhenPresent(t *testing.T) {
	schema := &model.SchemaRef{Type: "string"}
	traceArg := &model.ArgumentProfile{
		Type:         "string",
		Distribution: map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05},
	}
	g := buildComposed(schema, traceArg)
	for seed := range 5 {
		v := g.Example(seed).(string)
		if v != "GBP" && v != "USD" && v != "EUR" {
			t.Fatalf("seed %d: want trace support, got %q", seed, v)
		}
	}
}
