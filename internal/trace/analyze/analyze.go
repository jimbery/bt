// Package analyze builds TraceProfile statistics from HAR entries and OpenAPI operations.
package analyze

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/jimbery/bt/internal/trace/har"
	"github.com/jimbery/bt/pkg/model"
)

// ErrSequenceNormalization indicates Markov transition rows could not be normalised.
var ErrSequenceNormalization = errors.New("trace analyze: sequence normalization failed")

const (
	distributionMinSamples = 20
	maxSamplesPerArg       = 1000
	sessionGap             = 30 * time.Minute
)

type matchObs struct {
	op       *model.Operation
	entryIdx int
}

type argAgg struct {
	samples      []any
	nulls        int
	total        int
	missing      int
	stringCounts map[string]int
	numMin       *float64
	numMax       *float64
	inferredType string
}

func (ag *argAgg) addSample(v any) {
	if len(ag.samples) >= maxSamplesPerArg {
		return
	}
	ag.samples = append(ag.samples, v)
}

// Analyze matches HAR entries to operations and builds a TraceProfile.
// operations should be the discovered model.Operation list (same shape as bt uses at runtime).
// sourceHAR is the basename of the HAR file (stored in TraceProfile.SourceHAR).
func Analyze(entries []har.HAREntry, operations []model.Operation, sourceHAR string) (*model.TraceProfile, error) {
	profile := &model.TraceProfile{
		SchemaVersion: "1",
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		SourceHAR:     strings.TrimSpace(sourceHAR),
		Operations:    map[string]*model.OperationProfile{},
	}

	if len(entries) == 0 {
		return profile, nil
	}

	var matched []matchObs
	for i := range entries {
		e := &entries[i]
		op := matchOperation(e, operations)
		if op == nil {
			continue
		}
		matched = append(matched, matchObs{op: op, entryIdx: i})
	}

	if len(matched) == 0 {
		return profile, nil
	}

	opAggs := make(map[string]map[string]*argAgg)
	getAgg := func(opID, arg string) *argAgg {
		if opAggs[opID] == nil {
			opAggs[opID] = map[string]*argAgg{}
		}
		if opAggs[opID][arg] == nil {
			opAggs[opID][arg] = &argAgg{stringCounts: map[string]int{}}
		}
		return opAggs[opID][arg]
	}

	for _, m := range matched {
		e := entries[m.entryIdx]
		op := m.op
		if op.RequestBody == nil || len(op.RequestBody.Properties) == 0 {
			continue
		}
		var payload map[string]any
		if len(e.RequestBody) == 0 {
			for _, k := range fieldKeys(op.RequestBody) {
				ag := getAgg(op.ID, k)
				ag.total++
				ag.missing++
			}
			continue
		}
		if err := json.Unmarshal(e.RequestBody, &payload); err != nil {
			continue
		}
		for _, k := range fieldKeys(op.RequestBody) {
			ag := getAgg(op.ID, k)
			ag.total++
			v, ok := payload[k]
			if !ok || v == nil {
				ag.nulls++
				continue
			}
			prop := op.RequestBody.Properties[k]
			ag.inferredType = mergeType(ag.inferredType, inferType(prop, v))
			switch vv := v.(type) {
			case string:
				ag.stringCounts[vv]++
				ag.addSample(vv)
			case float64:
				ag.addSample(vv)
				f := vv
				if ag.numMin == nil || f < *ag.numMin {
					ag.numMin = ptrF(f)
				}
				if ag.numMax == nil || f > *ag.numMax {
					ag.numMax = ptrF(f)
				}
			case bool:
				ag.addSample(vv)
			default:
				ag.addSample(vv)
			}
		}
	}

	callCounts := make(map[string]int)
	for _, m := range matched {
		callCounts[m.op.ID]++
	}

	type rankRow struct {
		id    string
		count int
	}
	var ranks []rankRow
	for id, c := range callCounts {
		ranks = append(ranks, rankRow{id: id, count: c})
	}
	sort.Slice(ranks, func(i, j int) bool {
		if ranks[i].count != ranks[j].count {
			return ranks[i].count > ranks[j].count
		}
		return ranks[i].id < ranks[j].id
	})
	rankByID := make(map[string]int)
	for i, r := range ranks {
		rankByID[r.id] = i + 1
	}

	for opID, count := range callCounts {
		profile.Operations[opID] = &model.OperationProfile{
			CallCount:     count,
			FrequencyRank: rankByID[opID],
			Arguments:     map[string]*model.ArgumentProfile{},
		}
	}
	for opID, args := range opAggs {
		opProf := profile.Operations[opID]
		if opProf == nil {
			continue
		}
		for argName, ag := range args {
			ap := &model.ArgumentProfile{
				Type:          ag.inferredType,
				Samples:       append([]any(nil), ag.samples...),
				NullRate:      safeDiv(float64(ag.nulls), float64(ag.total)),
				AlwaysPresent: ag.missing == 0 && ag.nulls == 0 && ag.total > 0,
			}
			nonNull := ag.total - ag.nulls
			if nonNull >= distributionMinSamples && len(ag.stringCounts) > 0 {
				sum := 0
				for _, c := range ag.stringCounts {
					sum += c
				}
				if sum > 0 {
					ap.Distribution = map[string]float64{}
					for s, c := range ag.stringCounts {
						ap.Distribution[s] = float64(c) / float64(sum)
					}
				}
			}
			if ag.inferredType == "integer" || ag.inferredType == "number" {
				if ag.numMin != nil && ag.numMax != nil {
					ap.Range = &model.Range{Min: *ag.numMin, Max: *ag.numMax}
				}
			}
			opProf.Arguments[argName] = ap
		}
	}

	sessions := groupSessions(matched, entries)
	if err := buildSequences(profile, sessions); err != nil {
		return nil, err
	}

	return profile, nil
}

func ptrF(f float64) *float64 { return &f }

func safeDiv(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

func inferType(prop *model.SchemaRef, v any) string {
	if prop != nil && prop.Type != "" {
		switch prop.Type {
		case "integer", "number", "string", "boolean":
			return prop.Type
		}
	}
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		return "number"
	default:
		return "string"
	}
}

func fieldKeys(body *model.SchemaRef) []string {
	if body == nil || len(body.Properties) == 0 {
		return nil
	}
	keys := make([]string, 0, len(body.Properties))
	for k := range body.Properties {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func mergeType(cur, next string) string {
	if cur == "" {
		return next
	}
	if cur == next {
		return cur
	}
	if (cur == "integer" && next == "number") || (cur == "number" && next == "integer") {
		return "integer"
	}
	return next
}

func matchOperation(e *har.HAREntry, ops []model.Operation) *model.Operation {
	u, err := url.Parse(e.URL)
	if err != nil {
		return nil
	}
	path := u.Path
	if path == "" {
		path = "/"
	}
	for i := range ops {
		op := &ops[i]
		if strings.ToUpper(strings.TrimSpace(op.Method)) != e.Method {
			continue
		}
		if pathsMatch(op.Path, path) {
			return op
		}
	}
	return nil
}

func pathsMatch(template, concrete string) bool {
	ts := splitPath(template)
	cs := splitPath(concrete)
	if len(ts) != len(cs) {
		return false
	}
	for i := range ts {
		if isParam(ts[i]) {
			continue
		}
		if ts[i] != cs[i] {
			return false
		}
	}
	return true
}

func splitPath(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

func isParam(seg string) bool {
	return strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}")
}

// groupSessions returns ordered operation-ID chains per session.
func groupSessions(matched []matchObs, entries []har.HAREntry) [][]string {
	if len(matched) == 0 {
		return nil
	}
	order := append([]matchObs(nil), matched...)
	sort.Slice(order, func(i, j int) bool {
		ti := entries[order[i].entryIdx].StartedDateTime
		tj := entries[order[j].entryIdx].StartedDateTime
		if ti.Equal(tj) {
			return order[i].entryIdx < order[j].entryIdx
		}
		return ti.Before(tj)
	})

	var sessions [][]string
	var cur []string
	var lastTime time.Time
	var lastIP string
	var hasLast bool

	flush := func() {
		if len(cur) > 0 {
			sessions = append(sessions, cur)
			cur = nil
		}
	}

	for _, m := range order {
		e := entries[m.entryIdx]
		if !hasLast {
			cur = append(cur, m.op.ID)
			lastTime = e.StartedDateTime
			lastIP = e.ServerIPAddress
			hasLast = true
			continue
		}
		gap := e.StartedDateTime.Sub(lastTime)
		ipBreak := lastIP != "" && e.ServerIPAddress != "" && lastIP != e.ServerIPAddress
		if gap > sessionGap || ipBreak {
			flush()
			cur = append(cur, m.op.ID)
		} else {
			cur = append(cur, m.op.ID)
		}
		lastTime = e.StartedDateTime
		lastIP = e.ServerIPAddress
	}
	flush()
	return sessions
}

func buildSequences(profile *model.TraceProfile, sessions [][]string) error {
	if len(sessions) == 0 {
		return nil
	}
	startCounts := map[string]int{}
	transCounts := map[string]map[string]int{}

	addTrans := func(from, to string) {
		if transCounts[from] == nil {
			transCounts[from] = map[string]int{}
		}
		transCounts[from][to]++
	}

	minLen, maxLen := -1, -1
	for _, seq := range sessions {
		if len(seq) == 0 {
			continue
		}
		startCounts[seq[0]]++
		for i := 0; i < len(seq)-1; i++ {
			addTrans(seq[i], seq[i+1])
		}
		addTrans(seq[len(seq)-1], "__END__")
		l := len(seq)
		if minLen < 0 || l < minLen {
			minLen = l
		}
		if l > maxLen {
			maxLen = l
		}
	}
	if minLen < 0 {
		minLen = 0
	}

	sp, err := normaliseCounts(startCounts)
	if err != nil {
		return err
	}
	trans := map[string]map[string]float64{}
	for from, row := range transCounts {
		nr, err := normaliseCounts(row)
		if err != nil {
			return fmt.Errorf("%w: from %q: %v", ErrSequenceNormalization, from, err)
		}
		trans[from] = nr
	}

	profile.Sequences = &model.SequenceProfile{
		StartProbability:         sp,
		Transitions:              trans,
		MinObservedSessionLength: minLen,
		MaxObservedSessionLength: maxLen,
	}
	return nil
}

func normaliseCounts(counts map[string]int) (map[string]float64, error) {
	sum := 0
	for _, c := range counts {
		sum += c
	}
	if sum == 0 {
		return nil, ErrSequenceNormalization
	}
	out := make(map[string]float64, len(counts))
	for k, c := range counts {
		out[k] = float64(c) / float64(sum)
	}
	return out, validateProbMap(out, "row")
}

func validateProbMap(m map[string]float64, label string) error {
	t := 0.0
	for _, v := range m {
		t += v
	}
	if len(m) > 0 && (t < 0.999 || t > 1.001) {
		return fmt.Errorf("%s sums to %f", label, t)
	}
	return nil
}
