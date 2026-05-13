// Package gen builds stateful flows from trace Markov sequences (M13 / ADR-010).
package gen

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/jayimbery/bt/pkg/model"
)

// GenerateFlowsConfig controls trace-derived flow generation.
type GenerateFlowsConfig struct {
	Count    int
	MaxSteps int
}

// GenerateFlows samples Markov chains from a trace profile and returns concrete flows.
// ops supplies HTTP method/path templates per operation ID; when nil, GET "/" is used as a placeholder.
func GenerateFlows(profile *model.TraceProfile, ops []model.Operation, cfg GenerateFlowsConfig) []model.Flow {
	if profile == nil || profile.Sequences == nil || len(profile.Sequences.StartProbability) == 0 {
		return []model.Flow{}
	}
	max := cfg.MaxSteps
	if max <= 0 {
		max = profile.Sequences.MaxObservedSessionLength
		if max <= 0 {
			max = 5
		}
	}
	if max > 10 {
		max = 10
	}
	lookup := map[string]model.Operation{}
	for _, o := range ops {
		lookup[o.ID] = o
	}
	count := cfg.Count
	if count < 0 {
		count = 0
	}
	out := make([]model.Flow, 0, count)
	for i := 0; i < count; i++ {
		f := sampleFlow(profile, lookup, max, i+1)
		out = append(out, f)
	}
	return out
}

func sampleFlow(profile *model.TraceProfile, lookup map[string]model.Operation, maxSteps, idx int) model.Flow {
	start := sampleFromDist(profile.Sequences.StartProbability)
	steps := []model.FlowStep{makeStep(start, lookup, 0)}
	cur := start
	for len(steps) < maxSteps {
		row, ok := profile.Sequences.Transitions[cur]
		if !ok || len(row) == 0 {
			break
		}
		next := sampleFromDist(row)
		if next == "__END__" {
			break
		}
		steps = append(steps, makeStep(next, lookup, len(steps)))
		cur = next
	}
	return model.Flow{
		ID:          fmt.Sprintf("trace-flow-%d", idx),
		Description: "generated from trace profile",
		Steps:       steps,
	}
}

func makeStep(opID string, lookup map[string]model.Operation, stepIdx int) model.FlowStep {
	method, path := "GET", "/"
	if o, ok := lookup[opID]; ok {
		method = strings.TrimSpace(o.Method)
		if method == "" {
			method = "GET"
		}
		if strings.TrimSpace(o.Path) != "" {
			path = o.Path
		}
	}
	return model.FlowStep{
		ID:          fmt.Sprintf("auto-%d", stepIdx),
		OperationID: opID,
		Input: model.StepInput{
			Method: method,
			Path:   path,
		},
	}
}

func sampleFromDist(dist map[string]float64) string {
	if len(dist) == 0 {
		return ""
	}
	type kv struct {
		k string
		p float64
	}
	var rows []kv
	for k, v := range dist {
		if v > 0 {
			rows = append(rows, kv{k: k, p: v})
		}
	}
	if len(rows) == 0 {
		return ""
	}
	r := rand.Float64()
	acc := 0.0
	for _, row := range rows {
		acc += row.p
		if r <= acc {
			return row.k
		}
	}
	return rows[len(rows)-1].k
}
