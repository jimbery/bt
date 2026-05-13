package property

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"pgregory.net/rapid"

	"github.com/jayimbery/bt/internal/gqlcase"
	"github.com/jayimbery/bt/internal/strategy"
	gqlgen "github.com/jayimbery/bt/internal/strategy/graphql/gen"
	"github.com/jayimbery/bt/internal/strategy/property/gen"
	"github.com/jayimbery/bt/internal/strategy/property/invariant"
	"github.com/jayimbery/bt/pkg/model"
)

// rapidRunMu serialises property runs that touch Rapid's global command-line flags
// (rapid.checks, rapid.seed). Without this, parallel tests or concurrent Execute
// calls data-race on the flag package.
var rapidRunMu sync.Mutex

// rapidTestingOnce registers testing's flags and marks the stdlib flag package as parsed.
// Rapid's Check path calls testing.Short(); outside go test, short is nil and flag may be
// unparsed until this runs. See testing.Init and testing.Short.
var rapidTestingOnce sync.Once

func ensureStdlibTestingForRapid() {
	rapidTestingOnce.Do(func() {
		testing.Init()
		// Cobra uses pflag and does not mark the stdlib flag package parsed; Rapid's
		// engine calls testing.Short(), which requires flag.Parsed() in non-test binaries.
		if !testing.Testing() && !flag.Parsed() {
			_ = flag.CommandLine.Parse([]string{})
		}
	})
}

// ArtifactWriter persists failure bundles (same contract as replay.Writer).
type ArtifactWriter interface {
	Write(a model.Artifact) (string, error)
}

// Options configures optional behaviour for the property strategy.
type Options struct {
	ArtifactWriter ArtifactWriter
	Environment    string
}

type propertyStrategy struct {
	opts       Options
	ops        []model.Operation
	checks     int
	runSeed    uint64
	invariants []model.Invariant
}

// New returns a property Strategy with default options.
func New() strategy.Strategy {
	return &propertyStrategy{}
}

// NewWithOptions returns a property Strategy with artifact capture, etc.
func NewWithOptions(opts Options) strategy.Strategy {
	return &propertyStrategy{opts: opts}
}

func (s *propertyStrategy) Name() strategy.Kind { return strategy.KindProperty }

func (s *propertyStrategy) Plan(_ context.Context, spec strategy.Spec, ops []model.Operation) ([]model.Case, error) {
	s.checks = parseIntFromConfig(spec.Config, "checks", 100)
	if s.checks <= 0 {
		s.checks = 100
	}
	s.runSeed = resolveRunSeed(spec.Config)
	s.invariants = append([]model.Invariant(nil), spec.Invariants...)

	filtered := filterOperations(ops, spec.Operations)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("property strategy: no operations selected (check strategies[].operations in config)")
	}
	s.ops = filtered

	out := make([]model.Case, 0, len(filtered))
	for _, op := range filtered {
		out = append(out, model.Case{
			ID:          "property:" + op.ID,
			OperationID: op.ID,
			Input: model.CaseInput{
				Method: op.Method,
				Path:   fillPathParams(op),
			},
			Meta: map[string]any{"kind": "property"},
		})
	}
	return out, nil
}

func (s *propertyStrategy) Execute(ctx context.Context, cases []model.Case, exec strategy.Executor) ([]model.Result, error) {
	lookup := opByID(s.ops)
	results := make([]model.Result, 0, len(cases))
	for _, c := range cases {
		op, ok := lookup[c.OperationID]
		if !ok {
			return nil, fmt.Errorf("property strategy: unknown operation %q", c.OperationID)
		}
		res := s.runOneOperation(ctx, exec, c, op)
		results = append(results, res)
	}
	return results, nil
}

func (s *propertyStrategy) runOneOperation(ctx context.Context, exec strategy.Executor, c model.Case, op model.Operation) model.Result {
	rapidRunMu.Lock()
	defer rapidRunMu.Unlock()

	tb := newPropTB(c.ID)
	applyRapidFlags(s.checks, &s.runSeed)

	ensureStdlibTestingForRapid()

	start := time.Now()
	rapid.Check(tb, func(rt *rapid.T) {
		if err := ctx.Err(); err != nil {
			rt.Errorf("cancelled: %v", err)
			return
		}
		input := buildCaseInput(rt, op, c, s.invariants)
		resp, err := exec.Run(ctx, input)
		if err != nil {
			rt.Errorf("request failed: %v", err)
			return
		}
		var idem *model.IdempotencyResult
		if wantsInvariantName(s.invariants, model.InvariantIdempotencyKeyPreventsDupes) {
			key := ""
			if input.Headers != nil {
				key = input.Headers[invariant.HeaderKey()]
			}
			if key != "" {
				second, err2 := exec.Run(ctx, input)
				if err2 != nil {
					rt.Errorf("idempotent replay failed: %v", err2)
					return
				}
				ir := model.IdempotencyResult{IdempotencyKey: key, First: resp, Second: second}
				idem = &ir
				resp = second
			}
		}

		res := model.Result{
			CaseID:     c.ID,
			StatusCode: resp.StatusCode,
			Response:   resp,
			Request:    requestDetailFromInput(input),
		}
		failures := evaluateInvariants(op, res, s.invariants, idem)
		tb.snapshot(res.Request, resp, nil)
		if len(blockingFailures(failures)) > 0 {
			tb.snapshot(res.Request, resp, failures)
			rt.Errorf("property invariant failed (%d violation(s))", len(failures))
		}
	})
	dur := time.Since(start)

	req, resp, failures, failed := tb.exportState()
	shrinkCount := 0
	if failed {
		shrinkCount = 1
	}
	final := model.Result{
		CaseID:       c.ID,
		OperationID:  c.OperationID,
		StatusCode:   resp.StatusCode,
		Duration:     dur,
		Response:     resp,
		Request:      req,
		Passed:       !failed,
		Failures:     failures,
		StrategyKind: string(strategy.KindProperty),
		Seed:         int64(s.runSeed),
		CasesRun:     s.checks,
		ShrinkCount:  shrinkCount,
	}
	if !final.Passed && s.opts.ArtifactWriter != nil {
		artifact := model.Artifact{
			ID:           fmt.Sprintf("%s-%d", c.ID, time.Now().UnixNano()),
			StrategyKind: string(strategy.KindProperty),
			Seed:         int64(s.runSeed),
			CaseID:       c.ID,
			OccurredAt:   time.Now().UTC(),
			Environment:  s.opts.Environment,
			Request:      final.Request,
			Response:     final.Response,
			Failures:     final.Failures,
		}
		if gqlcase.IsGraphQLOperation(op) {
			artifact.GQLOperationKind = string(op.GQLKind)
			artifact.GQLVariables = map[string]any{}
			var payload map[string]any
			if len(final.Request.Body) > 0 && json.Unmarshal(final.Request.Body, &payload) == nil {
				if v, ok := payload["variables"].(map[string]any); ok && v != nil {
					artifact.GQLVariables = v
				}
			}
		}
		path, werr := s.opts.ArtifactWriter.Write(artifact)
		if werr != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: could not write property artifact: %v\n", werr)
		} else {
			final.ArtifactPath = path
		}
	}
	return final
}

func evaluateInvariants(op model.Operation, res model.Result, invs []model.Invariant, idem *model.IdempotencyResult) []model.Failure {
	var out []model.Failure
	schemaConfigured := false
	for _, inv := range invs {
		if inv.Name == model.InvariantResponseMatchesSchema {
			schemaConfigured = true
		}
		fn, ok := invariant.Lookup(inv.Name)
		if !ok {
			continue
		}
		out = append(out, fn(inv, op, res, idem)...)
	}
	if !schemaConfigured {
		out = append(out, invariant.ResponseMatchesSchema(op, res)...)
	}
	return out
}

func wantsInvariantName(invs []model.Invariant, name string) bool {
	for _, i := range invs {
		if i.Name == name {
			return true
		}
	}
	return false
}

func buildCaseInput(t *rapid.T, op model.Operation, c model.Case, invs []model.Invariant) model.CaseInput {
	if gqlcase.IsGraphQLOperation(op) {
		in := model.CaseInput{
			Method:   op.Method,
			Path:     gqlcase.FillPathParams(op),
			Headers:  map[string]string{"Content-Type": "application/json"},
			GQLQuery: op.GQLDocument,
		}
		if len(op.GQLVariableTypes) > 0 {
			in.GQLVariables = gqlgen.GenForOperation(op).Draw(t, "gql_vars")
		}
		if wantsInvariantName(invs, model.InvariantIdempotencyKeyPreventsDupes) {
			if in.Headers == nil {
				in.Headers = map[string]string{}
			}
			if rapid.Bool().Draw(t, "idem_present") {
				in.Headers[invariant.HeaderKey()] = fmt.Sprintf("idem-%016x", rapid.Uint64().Draw(t, "idem_key"))
			}
		}
		return in
	}

	in := c.Input
	if wantsRequestBody(op.Method) && op.RequestBody != nil {
		g := gen.GenForSchema(op.RequestBody)
		in.Body = g.Draw(t, "json_body")
	}
	if wantsInvariantName(invs, model.InvariantIdempotencyKeyPreventsDupes) {
		if in.Headers == nil {
			in.Headers = map[string]string{}
		}
		if rapid.Bool().Draw(t, "idem_present") {
			in.Headers[invariant.HeaderKey()] = fmt.Sprintf("idem-%016x", rapid.Uint64().Draw(t, "idem_key"))
		}
	}
	return in
}

func wantsRequestBody(method string) bool {
	switch strings.ToUpper(method) {
	case "POST", "PUT", "PATCH":
		return true
	default:
		return false
	}
}

func filterOperations(all []model.Operation, want []string) []model.Operation {
	if len(want) == 0 {
		out := make([]model.Operation, len(all))
		copy(out, all)
		return out
	}
	allowed := make(map[string]struct{}, len(want))
	for _, id := range want {
		allowed[id] = struct{}{}
	}
	var out []model.Operation
	for _, op := range all {
		if _, ok := allowed[op.ID]; ok {
			out = append(out, op)
		}
	}
	return out
}

func fillPathParams(op model.Operation) string {
	p := op.Path
	for _, param := range op.Parameters {
		if param.In != "path" {
			continue
		}
		ph := "{" + param.Name + "}"
		if !strings.Contains(p, ph) {
			continue
		}
		p = strings.ReplaceAll(p, ph, examplePathValue(param))
	}
	return p
}

func examplePathValue(p model.Parameter) string {
	if p.Schema == nil {
		return "1"
	}
	switch p.Schema.Type {
	case "integer", "number":
		return "1"
	case "string":
		if len(p.Schema.Enum) > 0 {
			return fmt.Sprint(p.Schema.Enum[0])
		}
		return "x"
	default:
		return "1"
	}
}

func opByID(ops []model.Operation) map[string]model.Operation {
	m := make(map[string]model.Operation, len(ops))
	for _, o := range ops {
		m[o.ID] = o
	}
	return m
}

func requestDetailFromInput(in model.CaseInput) model.RequestDetail {
	rd := model.RequestDetail{
		Method:  in.Method,
		URL:     in.Path,
		Headers: cloneStringMap(in.Headers),
		Query:   cloneStringMap(in.Query),
	}
	if in.IsGraphQL() {
		payload := map[string]any{"query": in.GQLQuery}
		if strings.TrimSpace(in.GQLOperationName) != "" {
			payload["operationName"] = in.GQLOperationName
		}
		if in.GQLVariables != nil {
			payload["variables"] = in.GQLVariables
		}
		if b, err := json.Marshal(payload); err == nil {
			rd.Body = b
		}
		return rd
	}
	if in.Body != nil {
		if b, err := json.Marshal(in.Body); err == nil {
			rd.Body = b
		}
	}
	return rd
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func parseIntFromConfig(cfg map[string]any, key string, def int) int {
	if cfg == nil {
		return def
	}
	v, ok := cfg[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return def
		}
		return int(i)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(x))
		if err != nil {
			return def
		}
		return n
	default:
		return def
	}
}

func resolveRunSeed(cfg map[string]any) uint64 {
	if cfg == nil {
		return randomNonZeroUint64()
	}
	v, ok := cfg["seed"]
	if !ok {
		return randomNonZeroUint64()
	}
	var u uint64
	switch x := v.(type) {
	case int:
		if x <= 0 {
			return randomNonZeroUint64()
		}
		u = uint64(x)
	case int64:
		if x <= 0 {
			return randomNonZeroUint64()
		}
		u = uint64(x)
	case float64:
		if x <= 0 {
			return randomNonZeroUint64()
		}
		u = uint64(x)
	case json.Number:
		i, err := x.Int64()
		if err != nil || i <= 0 {
			return randomNonZeroUint64()
		}
		u = uint64(i)
	case string:
		n, err := strconv.ParseUint(strings.TrimSpace(x), 10, 64)
		if err != nil || n == 0 {
			return randomNonZeroUint64()
		}
		u = n
	default:
		return randomNonZeroUint64()
	}
	return u
}

func randomNonZeroUint64() uint64 {
	var b [8]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			return uint64(time.Now().UnixNano())
		}
		u := binary.LittleEndian.Uint64(b[:])
		if u != 0 {
			return u
		}
	}
}

func applyRapidFlags(checks int, seed *uint64) {
	setFlag("rapid.checks", strconv.Itoa(checks))
	if seed != nil {
		setFlag("rapid.seed", strconv.FormatUint(*seed, 10))
	} else {
		setFlag("rapid.seed", "0")
	}
}

func setFlag(name, value string) {
	f := flag.Lookup(name)
	if f == nil {
		return
	}
	_ = f.Value.Set(value)
}

// propTB is a minimal rapid.TB for use outside go test.
type propTB struct {
	name         string
	failed       bool
	mu           sync.Mutex
	lastRequest  model.RequestDetail
	lastResponse model.ResponseDetail
	lastFailures []model.Failure
}

func newPropTB(name string) *propTB {
	return &propTB{name: name}
}

func (p *propTB) snapshot(req model.RequestDetail, resp model.ResponseDetail, failures []model.Failure) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastRequest = req
	p.lastResponse = resp
	if failures != nil {
		p.lastFailures = append([]model.Failure(nil), failures...)
	} else {
		p.lastFailures = nil
	}
}

// exportState returns a consistent snapshot of TB state for building model.Result
// after rapid.Check returns (avoids races with any concurrent TB callbacks).
func (p *propTB) exportState() (model.RequestDetail, model.ResponseDetail, []model.Failure, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	var failures []model.Failure
	if len(p.lastFailures) > 0 {
		failures = append([]model.Failure(nil), p.lastFailures...)
	}
	return p.lastRequest, p.lastResponse, failures, p.failed
}

func (p *propTB) Helper() {}

func (p *propTB) Name() string { return p.name }

func (p *propTB) Logf(string, ...any) {}

func (p *propTB) Log(...any) {}

func (p *propTB) Skipf(string, ...any) { panic("property: Skipf not supported") }

func (p *propTB) Skip(...any) { panic("property: Skip not supported") }

func (p *propTB) SkipNow() { panic("property: SkipNow not supported") }

func (p *propTB) Errorf(format string, args ...any) {
	p.mu.Lock()
	p.failed = true
	p.mu.Unlock()
	_, _ = fmt.Fprintf(os.Stderr, format+"\n", args...)
}

func (p *propTB) Error(args ...any) {
	p.mu.Lock()
	p.failed = true
	p.mu.Unlock()
	_, _ = fmt.Fprintln(os.Stderr, args...)
}

func (p *propTB) Fatalf(format string, args ...any) {
	p.Errorf(format, args...)
}

func (p *propTB) Fatal(args ...any) {
	p.Error(args...)
}

func (p *propTB) FailNow() {
	// rapid.Check calls FailNow after Errorf; a no-op is enough for non-testing TB.
}

func (p *propTB) Fail() {
	p.mu.Lock()
	p.failed = true
	p.mu.Unlock()
}

func (p *propTB) Failed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.failed
}

func blockingFailures(failures []model.Failure) []model.Failure {
	var out []model.Failure
	for _, f := range failures {
		if f.Classification == "graphql_warning" {
			continue
		}
		out = append(out, f)
	}
	return out
}
