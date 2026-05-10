package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/internal/strategy/property/gen"
	"github.com/jayimbery/bt/pkg/model"
)

// ArtifactWriter persists failure bundles (same contract as replay.Writer).
type ArtifactWriter interface {
	Write(a model.Artifact) (string, error)
}

// Options configures contract strategy behaviour.
type Options struct {
	ArtifactWriter ArtifactWriter
	Environment    string
	Logger         *slog.Logger
}

type contractStrategy struct {
	opts Options
	ops  []model.Operation
}

// New returns a Strategy implementing contract verification.
func New() strategy.Strategy { return &contractStrategy{} }

// NewWithOptions returns a contract Strategy with artifact capture.
func NewWithOptions(opts Options) strategy.Strategy {
	return &contractStrategy{opts: opts}
}

func (s *contractStrategy) Name() strategy.Kind { return strategy.KindContract }

func (s *contractStrategy) Plan(ctx context.Context, spec strategy.Spec, ops []model.Operation) ([]model.Case, error) {
	_ = ctx
	filtered := filterOperations(ops, spec.Operations)
	if len(filtered) == 0 {
		return nil, fmt.Errorf("contract strategy: no operations selected (check strategies[].operations in config)")
	}
	s.ops = filtered
	out := make([]model.Case, 0, len(filtered))
	for _, op := range filtered {
		out = append(out, model.Case{
			ID:          "contract:" + op.ID,
			OperationID: op.ID,
			Input:       buildMinimalInput(op),
			Meta:        map[string]any{"kind": "contract"},
		})
	}
	return out, nil
}

func (s *contractStrategy) Execute(ctx context.Context, cases []model.Case, exec strategy.Executor) ([]model.Result, error) {
	lookup := opByID(s.ops)
	results := make([]model.Result, 0, len(cases))
	for _, c := range cases {
		if err := ctx.Err(); err != nil {
			return results, err
		}
		op, ok := lookup[c.OperationID]
		if !ok {
			return nil, fmt.Errorf("contract strategy: unknown operation %q", c.OperationID)
		}
		res := s.runOne(ctx, exec, c, op)
		results = append(results, res)
	}
	return results, nil
}

func (s *contractStrategy) runOne(ctx context.Context, exec strategy.Executor, c model.Case, op model.Operation) model.Result {
	start := time.Now()
	resp, err := exec.Run(ctx, c.Input)
	dur := time.Since(start)

	rd := requestDetailFromInput(c.Input)
	if err != nil {
		return model.Result{
			CaseID:       c.ID,
			OperationID:  op.ID,
			Passed:       false,
			StrategyKind: string(strategy.KindContract),
			Duration:     dur,
			Request:      rd,
			Response:     model.ResponseDetail{},
			Failures: []model.Failure{{
				Invariant:      model.InvariantContract,
				Classification: "execution_error",
				Message:        err.Error(),
			}},
		}
	}

	schema := schemaForStatus(op, resp.StatusCode)
	refDisplay := ""
	if schema != nil {
		refDisplay = "openapi-response"
	}

	var violations []ContractViolation

	ct := ""
	if resp.Headers != nil {
		for k, v := range resp.Headers {
			if strings.EqualFold(k, "Content-Type") {
				ct = v
				break
			}
		}
	}
	needsJSONCT := len(resp.Body) > 0 || schema != nil
	if needsJSONCT && !strings.Contains(strings.ToLower(ct), "application/json") {
		violations = append(violations, ContractViolation{
			Field:    "Content-Type",
			Expected: "response Content-Type containing application/json",
			Actual:   ct,
			Severity: Critical,
		})
	}

	if !responseStatusDeclared(op, resp.StatusCode) {
		violations = append(violations, ContractViolation{
			Field:    "status_code",
			Expected: fmt.Sprintf("one of declared response codes: %v", declaredStatusCodes(op)),
			Actual:   strconv.Itoa(resp.StatusCode),
			Severity: Critical,
		})
	}

	if len(resp.Body) > 0 && schema != nil {
		var probe any
		if err := json.Unmarshal(resp.Body, &probe); err != nil {
			violations = append(violations, ContractViolation{
				Field:    "body",
				Expected: "valid JSON",
				Actual:   err.Error(),
				Severity: Critical,
			})
		} else {
			switch body := probe.(type) {
			case map[string]any:
				violations = append(violations, EvaluateBody(body, schema)...)
			default:
				violations = append(violations, EvaluateJSON(resp.Body, schema)...)
			}
		}
	}

	passed := true
	for _, v := range violations {
		if v.Severity == Critical {
			passed = false
			break
		}
	}

	failures := violationsToFailures(violations)
	res := model.Result{
		CaseID:            c.ID,
		OperationID:       op.ID,
		Passed:            passed,
		StrategyKind:      string(strategy.KindContract),
		StatusCode:        resp.StatusCode,
		Duration:          dur,
		Request:           rd,
		Response:          resp,
		Failures:          failures,
		ContractSchemaRef: refDisplay,
		CasesRun:          1,
	}

	if !passed && s.opts.ArtifactWriter != nil {
		art := model.Artifact{
			ID:           fmt.Sprintf("%s-%d", c.ID, time.Now().UnixNano()),
			StrategyKind: string(strategy.KindContract),
			CaseID:       c.ID,
			OccurredAt:   time.Now().UTC(),
			Environment:  s.opts.Environment,
			Request:      rd,
			Response:     resp,
			Failures:     failures,
		}
		path, werr := s.opts.ArtifactWriter.Write(art)
		if werr != nil {
			if s.opts.Logger != nil {
				s.opts.Logger.Warn("contract artifact", slog.String("error", werr.Error()))
			} else {
				_, _ = fmt.Fprintf(os.Stderr, "warning: could not write contract artifact: %v\n", werr)
			}
		} else {
			res.ArtifactPath = path
		}
	}
	return res
}

func violationsToFailures(vv []ContractViolation) []model.Failure {
	out := make([]model.Failure, 0, len(vv))
	for _, v := range vv {
		cl := ""
		if v.Severity == Warning {
			cl = "warning"
		}
		out = append(out, model.Failure{
			Invariant:      model.InvariantContract,
			Classification: cl,
			Message:        fmt.Sprintf("%s: expected %s; got %s", v.Field, v.Expected, v.Actual),
			Path:           v.Field,
			Expected:       v.Expected,
			Actual:         v.Actual,
		})
	}
	return out
}

func buildMinimalInput(op model.Operation) model.CaseInput {
	in := model.CaseInput{
		Method:  op.Method,
		Path:    fillPathParams(op),
		Headers: map[string]string{},
		Query:   map[string]string{},
	}
	switch strings.ToUpper(op.Method) {
	case "POST", "PUT", "PATCH":
		if op.RequestBody != nil {
			in.Body = gen.GenForSchema(op.RequestBody).Example(4242)
		}
	}
	return in
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

func schemaForStatus(op model.Operation, code int) *model.SchemaRef {
	for _, r := range op.Responses {
		if r.StatusCode == code {
			return r.Schema
		}
	}
	return nil
}

func responseStatusDeclared(op model.Operation, code int) bool {
	for _, r := range op.Responses {
		if r.StatusCode == code {
			return true
		}
	}
	return false
}

func declaredStatusCodes(op model.Operation) []int {
	out := make([]int, 0, len(op.Responses))
	for _, r := range op.Responses {
		out = append(out, r.StatusCode)
	}
	return out
}
