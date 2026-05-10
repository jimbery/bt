package fuzz

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jayimbery/bt/internal/gqlcase"
	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/internal/strategy/fuzz/classify"
	"github.com/jayimbery/bt/internal/strategy/fuzz/corpus"
	"github.com/jayimbery/bt/internal/strategy/fuzz/mutate"
	"github.com/jayimbery/bt/internal/strategy/fuzz/safety"
	"github.com/jayimbery/bt/pkg/model"
)

// ArtifactWriter persists failure bundles (same contract as replay.Writer).
type ArtifactWriter interface {
	Write(a model.Artifact) (string, error)
}

// Options configures fuzz strategy behaviour.
type Options struct {
	ArtifactWriter       ArtifactWriter
	Environment          string
	SafetyProfile        string
	AllowedMethods       []string
	DeniedMethods        []string
	MaxRequestsPerSecond float64
	MaxConcurrency       int
	TimeoutSeconds       float64
	DestructiveConfirmed bool
	// Logger receives per-operation INFO summaries when non-nil.
	Logger *slog.Logger
}

type fuzzStrategy struct {
	opts Options

	iterations int
	corpusDir  string
	seed       int64
	ops        []model.Operation
}

// New returns a fuzz Strategy with defaults.
func New() strategy.Strategy { return &fuzzStrategy{} }

// NewWithOptions returns a fuzz Strategy with artifact capture and safety wiring.
func NewWithOptions(opts Options) strategy.Strategy {
	return &fuzzStrategy{opts: opts}
}

func (s *fuzzStrategy) Name() strategy.Kind { return strategy.KindFuzz }

func (s *fuzzStrategy) Plan(_ context.Context, spec strategy.Spec, ops []model.Operation) ([]model.Case, error) {
	rc := model.RunConfigFromMap(spec.Config)
	s.iterations = rc.FuzzIterations
	if s.iterations <= 0 {
		s.iterations = parseIntFromConfig(spec.Config, "fuzz_iterations", 50)
	}
	if s.iterations <= 0 {
		s.iterations = 50
	}
	s.corpusDir = rc.CorpusDir
	if s.corpusDir == "" {
		s.corpusDir = parseStringFromConfig(spec.Config, "corpus_dir", "")
	}
	s.seed = parseInt64FromConfig(spec.Config, "fuzz_seed", time.Now().UnixNano())

	filtered := filterOperations(ops, spec.Operations)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("fuzz strategy: no operations selected (check strategies[].operations in config)")
	}
	s.ops = filtered

	out := make([]model.Case, 0, len(filtered))
	for _, op := range filtered {
		out = append(out, model.Case{
			ID:          "fuzz:" + op.ID,
			OperationID: op.ID,
			Input: model.CaseInput{
				Method: op.Method,
				Path:   fillPathParams(op),
			},
			Meta: map[string]any{"kind": "fuzz"},
		})
	}
	return out, nil
}

func (s *fuzzStrategy) Execute(ctx context.Context, cases []model.Case, exec strategy.Executor) ([]model.Result, error) {
	lookup := opByID(s.ops)
	prof := safety.Profile(strings.TrimSpace(strings.ToLower(s.opts.SafetyProfile)))
	if prof == "" {
		prof = safety.ProfileSafe
	}
	cfg := safety.SafetyConfig{
		Profile:              prof,
		AllowedMethods:       append([]string(nil), s.opts.AllowedMethods...),
		DeniedMethods:        append([]string(nil), s.opts.DeniedMethods...),
		MaxRequestsPerSecond: s.opts.MaxRequestsPerSecond,
		MaxConcurrency:       s.opts.MaxConcurrency,
		TimeoutSeconds:       s.opts.TimeoutSeconds,
	}
	var enfOpts []safety.Option
	if cfg.Profile == safety.ProfileDestructive {
		enfOpts = append(enfOpts, safety.WithDestructiveConfirmed(s.opts.DestructiveConfirmed))
	}
	enf, err := safety.NewEnforcer(cfg, enfOpts...)
	if err != nil {
		return nil, err
	}

	rng := rand.New(rand.NewSource(s.seed))
	results := make([]model.Result, 0, len(cases))
	for _, c := range cases {
		op, ok := lookup[c.OperationID]
		if !ok {
			return nil, fmt.Errorf("fuzz strategy: unknown operation %q", c.OperationID)
		}
		res := s.runOneOperation(ctx, exec, enf, rng, c, op)
		results = append(results, res)
	}
	return results, nil
}

func (s *fuzzStrategy) runOneOperation(ctx context.Context, exec strategy.Executor, enf *safety.Enforcer, rng *rand.Rand, c model.Case, op model.Operation) model.Result {
	if !enf.Allow(op.Method) {
		prof := strings.TrimSpace(s.opts.SafetyProfile)
		if prof == "" {
			prof = "safe"
		}
		return model.Result{
			CaseID:       c.ID,
			Passed:       false,
			Skipped:      true,
			SkipReason:   fmt.Sprintf("method blocked by safety profile (%s)", strings.TrimSpace(prof)),
			StrategyKind: string(strategy.KindFuzz),
		}
	}

	var seeds []mutate.Input
	if s.corpusDir != "" {
		cp := corpus.NewCorpus(s.corpusDir)
		loaded, err := cp.Load()
		if err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "warning: fuzz corpus load: %v\n", err)
		} else {
			seeds = loaded
		}
	}
	if len(seeds) == 0 {
		seeds = builtinSeedCorpus(enf, op)
	}

	var muts []mutate.Mutator
	if gqlcase.IsGraphQLOperation(op) {
		muts = []mutate.Mutator{mutate.NewPayloadMutator()}
	} else {
		muts = []mutate.Mutator{
			mutate.NewPayloadMutator(),
			mutate.NewHeaderMutator(),
			mutate.NewPathMutator(),
			mutate.NewQueryMutator(),
		}
	}
	ms := mutate.NewMutatorSet(muts...)

	texec := &timeoutExecutor{inner: exec, d: enf.RequestTimeout()}
	var failures []model.Failure
	var lastReq model.RequestDetail
	var lastResp model.ResponseDetail
	reqCount := 0
	throttle := enf.ThrottleDelay()
	start := time.Now()

outer:
	for _, seedIn := range seeds {
		merged := mergeSeedWithOperation(seedIn, op)
		variants := ms.MutateAll(merged, rng)
		for _, variant := range variants {
			if reqCount >= s.iterations {
				break outer
			}
			select {
			case <-ctx.Done():
				break outer
			default:
			}
			if throttle > 0 && reqCount > 0 {
				time.Sleep(throttle)
			}
			ci := mutate.CaseInputFrom(variant)
			subctx, cancel := context.WithTimeout(ctx, enf.RequestTimeout())
			resp, err := texec.Run(subctx, ci)
			cancel()
			reqCount++
			lastReq = requestDetailFromInput(ci)
			lastResp = resp

			class := classify.ClassifyDetail(resp, err, op)
			if class == classify.ClassificationPass {
				continue
			}
			msg := classify.Message(class, err)
			fail := model.Failure{
				Invariant:      "fuzz",
				Classification: string(class),
				Message:        msg,
				MutatedInput:   formatMutatedInput(variant),
			}
			if s.opts.ArtifactWriter != nil {
				art := model.Artifact{
					ID:           fmt.Sprintf("%s-%d", c.ID, time.Now().UnixNano()),
					StrategyKind: string(strategy.KindFuzz),
					Seed:         s.seed,
					CaseID:       c.ID,
					OccurredAt:   time.Now().UTC(),
					Environment:  s.opts.Environment,
					Request:      lastReq,
					Response:     resp,
					Failures:     []model.Failure{fail},
				}
				path, werr := s.opts.ArtifactWriter.Write(art)
				if werr != nil {
					_, _ = fmt.Fprintf(os.Stderr, "warning: could not write fuzz artifact: %v\n", werr)
				} else {
					fail.ArtifactPath = path
				}
			}
			failures = append(failures, fail)
			if s.corpusDir != "" {
				if err := corpus.NewCorpus(s.corpusDir).Save(variant); err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "warning: could not save fuzz corpus entry: %v\n", err)
				}
			}
		}
	}

	dur := time.Since(start)
	passed := len(failures) == 0
	res := model.Result{
		CaseID:        c.ID,
		OperationID:   c.OperationID,
		Passed:        passed,
		StrategyKind:  string(strategy.KindFuzz),
		MutationCount: reqCount,
		StatusCode:    lastResp.StatusCode,
		Duration:      dur,
		Request:       lastReq,
		Response:      lastResp,
		Failures:      failures,
	}
	if !passed && s.opts.ArtifactWriter != nil && len(failures) > 0 && failures[0].ArtifactPath != "" {
		res.ArtifactPath = failures[0].ArtifactPath
	}
	if s.opts.Logger != nil {
		s.opts.Logger.Info("fuzz operation complete",
			slog.String("case_id", c.ID),
			slog.String("operation_id", op.ID),
			slog.Int("mutations", reqCount),
			slog.Int("failures", len(failures)),
		)
	}
	return res
}

func mergeSeedWithOperation(seed mutate.Input, op model.Operation) mutate.Input {
	out := mutate.Clone(seed)
	if strings.TrimSpace(out.Method) == "" {
		out.Method = op.Method
	}
	if strings.TrimSpace(out.Path) == "" {
		out.Path = fillPathParams(op)
	}
	if out.Query == nil {
		out.Query = map[string]string{}
	}
	if out.Headers == nil {
		out.Headers = map[string]string{}
	}
	if gqlcase.IsGraphQLOperation(op) {
		if strings.TrimSpace(out.GQLQuery) == "" {
			out.GQLQuery = op.GQLDocument
		}
		if out.GQLVariables == nil && len(op.GQLVariableTypes) > 0 {
			out.GQLVariables = gqlcase.ExampleVariables(op)
		}
	}
	return out
}

// builtinSeedCorpus returns a single seed using this operation's declared HTTP method
// (from OpenAPI) and resolved path. We only fuzz the method the operation actually
// defines — sending POST/PATCH to a GET-only path produces 405s that are noise for
// this strategy and break CI smoke runs.
func builtinSeedCorpus(enf *safety.Enforcer, op model.Operation) []mutate.Input {
	if gqlcase.IsGraphQLOperation(op) {
		m := strings.ToUpper(strings.TrimSpace(op.Method))
		if m == "" {
			m = "POST"
		}
		if !enf.Allow(m) {
			return nil
		}
		in := mutate.Input{
			Method:       m,
			Path:         fillPathParams(op),
			Headers:      map[string]string{"Content-Type": "application/json"},
			GQLQuery:     op.GQLDocument,
			GQLVariables: gqlcase.ExampleVariables(op),
		}
		return []mutate.Input{in}
	}
	path := fillPathParams(op)
	m := strings.ToUpper(strings.TrimSpace(op.Method))
	if m == "" {
		m = "GET"
	}
	if !enf.Allow(m) {
		return nil
	}
	return []mutate.Input{seedInputForMethod(m, path)}
}

func seedInputForMethod(method, path string) mutate.Input {
	in := mutate.Input{
		Method: method,
		Path:   path,
		Query:  map[string]string{},
	}
	switch method {
	case "GET", "DELETE", "HEAD", "OPTIONS":
		in.Headers = map[string]string{}
	default:
		in.Headers = map[string]string{"Content-Type": "application/json"}
		in.Body = []byte(`{}`)
	}
	return in
}

type timeoutExecutor struct {
	inner strategy.Executor
	d     time.Duration
}

func (t *timeoutExecutor) Run(ctx context.Context, in model.CaseInput) (model.ResponseDetail, error) {
	c2, cancel := context.WithTimeout(ctx, t.d)
	defer cancel()
	return t.inner.Run(c2, in)
}

func formatMutatedInput(in mutate.Input) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", strings.ToUpper(strings.TrimSpace(in.Method)), in.Path)
	if len(in.Query) > 0 {
		b.WriteByte('?')
		keys := make([]string, 0, len(in.Query))
		for k := range in.Query {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			if i > 0 {
				b.WriteByte('&')
			}
			fmt.Fprintf(&b, "%s=%s", k, in.Query[k])
		}
	}
	return b.String()
}

func requestDetailFromInput(in model.CaseInput) model.RequestDetail {
	rd := model.RequestDetail{
		Method:  in.Method,
		URL:     in.Path,
		Headers: cloneStringMap(in.Headers),
		Query:   cloneStringMap(in.Query),
	}
	if in.Body != nil {
		if raw, ok := in.Body.(json.RawMessage); ok {
			rd.Body = append([]byte(nil), raw...)
		} else if b, err := json.Marshal(in.Body); err == nil {
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

func opByID(ops []model.Operation) map[string]model.Operation {
	m := make(map[string]model.Operation, len(ops))
	for _, o := range ops {
		m[o.ID] = o
	}
	return m
}

func filterOperations(all []model.Operation, want []string) []model.Operation {
	if len(want) == 0 {
		return append([]model.Operation(nil), all...)
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

func parseInt64FromConfig(cfg map[string]any, key string, def int64) int64 {
	if cfg == nil {
		return def
	}
	v, ok := cfg[key]
	if !ok {
		return def
	}
	switch x := v.(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return def
		}
		return i
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return def
		}
		return n
	default:
		return def
	}
}

func parseStringFromConfig(cfg map[string]any, key, def string) string {
	if cfg == nil {
		return def
	}
	v, ok := cfg[key]
	if !ok {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	return s
}
