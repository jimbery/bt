package stateful

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jimbery/bt/internal/strategy"
	"github.com/jimbery/bt/internal/strategy/stateful/gen"
	"github.com/jimbery/bt/internal/strategy/stateful/loader"
	"github.com/jimbery/bt/pkg/model"
)

const metaFlowJSON = "bt_stateful_flow_json"

// Options configures the stateful strategy (M13).
type Options struct {
	ArtifactWriter   ArtifactWriter
	Environment      string
	BaseURL          string
	ConfigDir        string
	TraceProfilePath string
}

type statefulStrategy struct {
	opts Options
}

// NewStrategy returns a Strategy implementation for stateful flows.
func NewStrategy(opts Options) strategy.Strategy {
	return &statefulStrategy{opts: opts}
}

func (s *statefulStrategy) Name() strategy.Kind { return strategy.KindStateful }

func (s *statefulStrategy) Plan(ctx context.Context, spec strategy.Spec, ops []model.Operation) ([]model.Case, error) {
	_ = ctx
	flows, err := s.loadFlows(spec, ops)
	if err != nil {
		return nil, err
	}
	flows = filterFlowsByOperations(flows, spec.Operations)
	if len(flows) == 0 {
		return nil, errors.New("stateful: no flows to run (check file, flows_dir, trace_flow_count, and operations filter)")
	}
	var cases []model.Case
	for _, flow := range flows {
		meta, err := encodeFlowMeta(flow)
		if err != nil {
			return nil, fmt.Errorf("flow %q: %w", flow.ID, err)
		}
		cases = append(cases, model.Case{
			ID:          flow.ID,
			OperationID: flow.ID,
			Meta:        meta,
		})
	}
	return cases, nil
}

func (s *statefulStrategy) loadFlows(spec strategy.Spec, ops []model.Operation) ([]model.Flow, error) {
	var flows []model.Flow
	cfg := spec.Config
	if cfg == nil {
		cfg = map[string]any{}
	}

	file := strings.TrimSpace(stringFrom(cfg["file"]))
	if file == "" {
		file = strings.TrimSpace(stringFrom(cfg["flows_file"]))
	}
	if file != "" {
		path := resolvePath(s.opts.ConfigDir, file)
		ff, err := loader.LoadFlowsFile(path)
		if err != nil {
			return nil, fmt.Errorf("stateful: load flows file %q: %w", path, err)
		}
		flows = append(flows, ff...)
	}

	dir := strings.TrimSpace(stringFrom(cfg["flows_dir"]))
	if dir != "" {
		path := resolvePath(s.opts.ConfigDir, dir)
		ff, err := loader.LoadFlowsDir(path)
		if err != nil {
			return nil, fmt.Errorf("stateful: load flows dir %q: %w", path, err)
		}
		flows = append(flows, ff...)
	}

	n := intFrom(cfg["trace_flow_count"])
	if n <= 0 {
		n = intFrom(cfg["trace_flows"])
	}
	if n > 0 {
		profPath := strings.TrimSpace(s.opts.TraceProfilePath)
		if profPath == "" {
			return nil, errors.New("stateful: trace_flow_count is set but no trace profile path is available")
		}
		if _, err := os.Stat(profPath); err != nil {
			return nil, fmt.Errorf("stateful: trace profile %q: %w", profPath, err)
		}
		prof, err := model.ParseProfile(profPath)
		if err != nil {
			return nil, fmt.Errorf("stateful: parse trace profile: %w", err)
		}
		maxSteps := intFrom(cfg["trace_max_steps"])
		generated := gen.GenerateFlows(prof, ops, gen.GenerateFlowsConfig{Count: n, MaxSteps: maxSteps})
		flows = append(flows, generated...)
	}

	if len(flows) == 0 {
		return nil, errors.New("stateful: configure file, flows_dir, or trace_flow_count with a valid trace profile")
	}
	return flows, nil
}

func (s *statefulStrategy) Execute(ctx context.Context, cases []model.Case, exec strategy.Executor) ([]model.Result, error) {
	r := NewRunner(Config{
		BaseURL:        s.opts.BaseURL,
		ArtifactWriter: s.opts.ArtifactWriter,
		Environment:    s.opts.Environment,
	})
	var results []model.Result
	for _, c := range cases {
		flow, err := decodeFlowMeta(c.Meta)
		if err != nil {
			return nil, fmt.Errorf("case %q: %w", c.ID, err)
		}
		frs, err := r.Execute(ctx, []model.Flow{flow}, exec)
		if err != nil {
			return nil, fmt.Errorf("case %q: %w", c.ID, err)
		}
		if len(frs) != 1 {
			return nil, fmt.Errorf("case %q: expected one flow result", c.ID)
		}
		results = append(results, flowResultToModelResult(frs[0]))
	}
	return results, nil
}

func encodeFlowMeta(flow model.Flow) (map[string]any, error) {
	b, err := json.Marshal(flow)
	if err != nil {
		return nil, err
	}
	return map[string]any{metaFlowJSON: string(b)}, nil
}

func decodeFlowMeta(meta map[string]any) (model.Flow, error) {
	if meta == nil {
		return model.Flow{}, errors.New("missing meta")
	}
	raw, _ := meta[metaFlowJSON].(string)
	if strings.TrimSpace(raw) == "" {
		return model.Flow{}, errors.New("missing bt_stateful_flow_json in case meta")
	}
	var f model.Flow
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		return model.Flow{}, err
	}
	return f, nil
}

func resolvePath(configDir, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if strings.TrimSpace(configDir) == "" {
		return p
	}
	return filepath.Join(configDir, p)
}

func stringFrom(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return ""
	}
}

func intFrom(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case json.Number:
		n, _ := x.Int64()
		return int(n)
	default:
		return 0
	}
}

func filterFlowsByOperations(flows []model.Flow, ops []string) []model.Flow {
	if len(ops) == 0 {
		return flows
	}
	allow := map[string]struct{}{}
	for _, o := range ops {
		o = strings.TrimSpace(o)
		if o != "" {
			allow[o] = struct{}{}
		}
	}
	var out []model.Flow
outer:
	for _, f := range flows {
		for _, st := range f.Steps {
			id := strings.TrimSpace(st.OperationID)
			if id == "" {
				continue
			}
			if _, ok := allow[id]; !ok {
				continue outer
			}
		}
		out = append(out, f)
	}
	return out
}
