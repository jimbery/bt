package stateful

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jimbery/bt/internal/runner"
	"github.com/jimbery/bt/internal/strategy"
	"github.com/jimbery/bt/internal/strategy/stateful/binding"
	"github.com/jimbery/bt/internal/strategy/table"
	"github.com/jimbery/bt/pkg/model"
)

// ArtifactWriter persists stateful failure bundles (same contract as replay.Writer).
type ArtifactWriter interface {
	Write(a model.Artifact) (string, error)
}

// Config configures the stateful HTTP runner.
type Config struct {
	BaseURL        string
	ArtifactWriter ArtifactWriter
	Environment    string
}

// Runner executes flows over HTTP (M13).
type Runner struct {
	cfg Config
}

// NewRunner returns a runner for stateful flows.
func NewRunner(cfg Config) *Runner { return &Runner{cfg: cfg} }

// Execute runs flows sequentially. When exec is nil, uses runner.New with cfg.BaseURL.
func (r *Runner) Execute(ctx context.Context, flows []model.Flow, exec strategy.Executor) ([]model.FlowResult, error) {
	if exec == nil {
		exec = runner.New(runner.Config{BaseURL: r.cfg.BaseURL, Timeout: runner.DefaultTimeout})
	}
	out := make([]model.FlowResult, 0, len(flows))
	for i := range flows {
		out = append(out, r.runOneFlow(ctx, &flows[i], exec))
	}
	return out, nil
}

func mergeExtractSpecs(step *model.FlowStep, accumulated map[string]model.ExtractSpec) map[string]model.ExtractSpec {
	out := make(map[string]model.ExtractSpec)
	for k, v := range accumulated {
		out[k] = v
	}
	for k, v := range step.Extract {
		out[k] = v
	}
	return out
}

func (r *Runner) runOneFlow(ctx context.Context, flow *model.Flow, exec strategy.Executor) model.FlowResult {
	if flow == nil {
		return model.FlowResult{FlowID: "", Passed: false, Steps: nil}
	}
	bindings := map[string]any{}
	extractHints := map[string]model.ExtractSpec{}
	var stepResults []model.StepResult
	allPassed := true

	for si := range flow.Steps {
		step := &flow.Steps[si]
		injectStep := *step
		injectStep.Extract = mergeExtractSpecs(step, extractHints)
		resolvedIn, err := binding.Inject(&injectStep, bindings)
		if err != nil {
			sr := model.StepResult{
				StepID:           step.ID,
				OperationID:      step.OperationID,
				Passed:           false,
				StatusCode:       0,
				Request:          resolvedInputToModel(resolvedIn, step),
				Response:         model.StepResponse{},
				Bindings:         map[string]any{},
				SchemaViolations: []model.SchemaViolation{},
				BindingFailure: &model.BindingFailure{
					Key:        "_inject",
					Expression: "",
					Severity:   "Critical",
					Message:    err.Error(),
				},
			}
			allPassed = false
			out := append(append([]model.StepResult(nil), stepResults...), sr)
			return r.finalizeFlow(flow, allPassed, out, &sr)
		}

		in := resolvedInputToCaseInput(resolvedIn)
		resp, err := exec.Run(ctx, in)
		if err != nil {
			sr := model.StepResult{
				StepID:           step.ID,
				OperationID:      step.OperationID,
				Passed:           false,
				StatusCode:       0,
				Request:          bindingResolvedToModel(resolvedIn),
				Response:         model.StepResponse{},
				Bindings:         map[string]any{},
				SchemaViolations: []model.SchemaViolation{},
			}
			allPassed = false
			out := append(append([]model.StepResult(nil), stepResults...), sr)
			return r.finalizeFlow(flow, allPassed, out, &sr)
		}

		sresp := responseDetailToStep(resp)
		sr := model.StepResult{
			StepID:           step.ID,
			OperationID:      step.OperationID,
			StatusCode:       resp.StatusCode,
			Request:          bindingResolvedToModel(resolvedIn),
			Response:         sresp,
			SchemaViolations: []model.SchemaViolation{},
			Bindings:         map[string]any{},
		}

		stepPassed := true
		if step.Expected != nil && step.Expected.StatusCode != 0 && resp.StatusCode != step.Expected.StatusCode {
			stepPassed = false
			allPassed = false
		}
		if step.Expected != nil && step.Expected.Schema != nil {
			sv := table.EvaluateSchema(step.Expected.Schema, resp.Body)
			sr.SchemaViolations = sv
			if len(sv) > 0 {
				stepPassed = false
				allPassed = false
			}
		}

		for key, spec := range step.Extract {
			val, xerr := binding.Extract(spec.From, sresp)
			if xerr != nil {
				bf := &model.BindingFailure{
					Key:          key,
					Expression:   spec.From,
					Severity:     "Critical",
					Message:      xerr.Error(),
					ResponseBody: append([]byte(nil), sresp.Body...),
				}
				sr.BindingFailure = bf
				sr.Passed = false
				allPassed = false
				out := append(append([]model.StepResult(nil), stepResults...), sr)
				return r.finalizeFlow(flow, allPassed, out, &sr)
			}
			bindings[key] = val
			sr.Bindings[key] = val
		}

		for k, spec := range step.Extract {
			extractHints[k] = spec
		}

		sr.Passed = stepPassed
		stepResults = append(stepResults, sr)
	}

	return r.finalizeFlow(flow, allPassed, stepResults, nil)
}

func (r *Runner) finalizeFlow(flow *model.Flow, passed bool, steps []model.StepResult, last *model.StepResult) model.FlowResult {
	fr := model.FlowResult{
		FlowID: flow.ID,
		Passed: passed,
		Steps:  steps,
	}
	if passed || r.cfg.ArtifactWriter == nil {
		return fr
	}
	path := r.writeArtifact(flow, steps, last)
	fr.ArtifactPath = path
	return fr
}

func (r *Runner) writeArtifact(flow *model.Flow, steps []model.StepResult, last *model.StepResult) string {
	flowCopy := *flow
	a := model.Artifact{
		ID:             fmt.Sprintf("%s-%d", flow.ID, time.Now().UnixNano()),
		StrategyKind:   string(strategy.KindStateful),
		CaseID:         flow.ID,
		OccurredAt:     time.Now().UTC(),
		Environment:    r.cfg.Environment,
		Request:        model.RequestDetail{Method: "FLOW", URL: flow.ID},
		Response:       model.ResponseDetail{},
		StatefulFlow:   &flowCopy,
		StatefulResult: &model.FlowResult{FlowID: flow.ID, Passed: false, Steps: steps},
	}
	a.Failures = artifactFailures(steps)
	if last != nil {
		a.Request = resolvedToRequestDetail(last.Request)
		a.Response = stepResponseToDetail(last.Response)
	} else if len(steps) > 0 {
		ls := steps[len(steps)-1]
		a.Request = resolvedToRequestDetail(ls.Request)
		a.Response = stepResponseToDetail(ls.Response)
	}
	path, err := r.cfg.ArtifactWriter.Write(a)
	if err != nil {
		return ""
	}
	return path
}

func artifactFailures(steps []model.StepResult) []model.Failure {
	var ff []model.Failure
	for _, st := range steps {
		if st.BindingFailure != nil {
			ff = append(ff, model.Failure{
				Invariant: model.InvariantStatefulBinding,
				Message:   st.BindingFailure.Message,
				Path:      st.BindingFailure.Key,
			})
		}
		if !st.Passed && st.BindingFailure == nil {
			if len(st.SchemaViolations) > 0 {
				ff = append(ff, model.Failure{
					Invariant: model.InvariantStatefulSchema,
					Message:   fmt.Sprintf("step %q: %d schema violation(s)", st.StepID, len(st.SchemaViolations)),
				})
			} else if st.StatusCode != 0 {
				ff = append(ff, model.Failure{
					Invariant: model.InvariantStatefulStatus,
					Message:   fmt.Sprintf("step %q: HTTP status %d", st.StepID, st.StatusCode),
				})
			}
		}
	}
	return ff
}

// Replay re-executes a saved flow using binding values recorded in the artifact (M13).
func (r *Runner) Replay(ctx context.Context, artifactPath string) (*model.FlowResult, error) {
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return nil, err
	}
	var a model.Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return r.ReplayArtifact(ctx, &a)
}

// ReplayArtifact re-executes a stateful flow from an in-memory artifact (CLI replay).
func (r *Runner) ReplayArtifact(ctx context.Context, a *model.Artifact) (*model.FlowResult, error) {
	if a == nil || a.StatefulFlow == nil || a.StatefulResult == nil {
		return nil, fmt.Errorf("artifact is not a replayable stateful bundle")
	}
	exec := runner.New(runner.Config{BaseURL: r.cfg.BaseURL, Timeout: runner.DefaultTimeout})
	return r.replayWithArtifact(ctx, a.StatefulFlow, a.StatefulResult, exec)
}

func (r *Runner) replayWithArtifact(ctx context.Context, flow *model.Flow, saved *model.FlowResult, exec strategy.Executor) (*model.FlowResult, error) {
	extractHints := map[string]model.ExtractSpec{}
	var out []model.StepResult
	allPassed := true

	for si := range flow.Steps {
		step := &flow.Steps[si]
		bindings := map[string]any{}
		for j := 0; j < si && j < len(saved.Steps); j++ {
			for k, v := range saved.Steps[j].Bindings {
				bindings[k] = v
			}
		}

		injectStep := *step
		injectStep.Extract = mergeExtractSpecs(step, extractHints)
		resolvedIn, err := binding.Inject(&injectStep, bindings)
		if err != nil {
			return nil, err
		}
		in := resolvedInputToCaseInput(resolvedIn)
		resp, err := exec.Run(ctx, in)
		if err != nil {
			return nil, err
		}
		sresp := responseDetailToStep(resp)

		sr := model.StepResult{
			StepID:           step.ID,
			OperationID:      step.OperationID,
			StatusCode:       resp.StatusCode,
			Request:          bindingResolvedToModel(resolvedIn),
			Response:         sresp,
			SchemaViolations: []model.SchemaViolation{},
			Bindings:         map[string]any{},
		}

		stepPassed := true
		if step.Expected != nil && step.Expected.StatusCode != 0 && resp.StatusCode != step.Expected.StatusCode {
			stepPassed = false
			allPassed = false
		}
		if step.Expected != nil && step.Expected.Schema != nil {
			sv := table.EvaluateSchema(step.Expected.Schema, resp.Body)
			sr.SchemaViolations = sv
			if len(sv) > 0 {
				stepPassed = false
				allPassed = false
			}
		}

		if si < len(saved.Steps) {
			for k, v := range saved.Steps[si].Bindings {
				sr.Bindings[k] = v
			}
		}
		for k, spec := range step.Extract {
			extractHints[k] = spec
		}
		sr.Passed = stepPassed
		out = append(out, sr)
	}

	return &model.FlowResult{FlowID: flow.ID, Passed: allPassed, Steps: out}, nil
}

func resolvedInputToModel(in *binding.ResolvedInput, step *model.FlowStep) model.ResolvedRequest {
	if in != nil {
		return bindingResolvedToModel(in)
	}
	h := http.Header{}
	for k, v := range step.Input.Headers {
		h.Set(k, v)
	}
	q := map[string]string{}
	for k, v := range step.Input.Query {
		q[k] = v
	}
	return model.ResolvedRequest{
		Method:      strings.TrimSpace(step.Input.Method),
		Path:        step.Input.Path,
		Headers:     h,
		QueryParams: q,
	}
}

func bindingResolvedToModel(in *binding.ResolvedInput) model.ResolvedRequest {
	if in == nil {
		return model.ResolvedRequest{Headers: http.Header{}}
	}
	h := in.Headers.Clone()
	if h == nil {
		h = http.Header{}
	}
	q := map[string]string{}
	for k, v := range in.QueryParams {
		q[k] = v
	}
	return model.ResolvedRequest{
		Method:      in.Method,
		Path:        in.Path,
		Headers:     h,
		QueryParams: q,
		Body:        append([]byte(nil), in.Body...),
	}
}

func resolvedInputToCaseInput(in *binding.ResolvedInput) model.CaseInput {
	if in == nil {
		return model.CaseInput{}
	}
	hdr := map[string]string{}
	for k, vs := range in.Headers {
		if len(vs) > 0 {
			hdr[k] = vs[0]
		}
	}
	ci := model.CaseInput{
		Method:  in.Method,
		Path:    in.Path,
		Headers: hdr,
		Query:   in.QueryParams,
	}
	if len(in.Body) > 0 {
		ci.Body = json.RawMessage(append(json.RawMessage(nil), in.Body...))
	}
	return ci
}

func responseDetailToStep(d model.ResponseDetail) model.StepResponse {
	h := http.Header{}
	for k, v := range d.Headers {
		h.Set(k, v)
	}
	return model.StepResponse{
		StatusCode: d.StatusCode,
		Headers:    h,
		Body:       append([]byte(nil), d.Body...),
	}
}

func resolvedToRequestDetail(rr model.ResolvedRequest) model.RequestDetail {
	h := map[string]string{}
	for k, vs := range rr.Headers {
		if len(vs) > 0 {
			h[k] = vs[0]
		}
	}
	return model.RequestDetail{
		Method:  rr.Method,
		URL:     rr.Path,
		Headers: h,
		Query:   rr.QueryParams,
		Body:    append([]byte(nil), rr.Body...),
	}
}

func stepResponseToDetail(sr model.StepResponse) model.ResponseDetail {
	h := map[string]string{}
	for k, vs := range sr.Headers {
		if len(vs) > 0 {
			h[k] = vs[0]
		}
	}
	return model.ResponseDetail{
		StatusCode: sr.StatusCode,
		Headers:    h,
		Body:       append([]byte(nil), sr.Body...),
	}
}

func flowResultToModelResult(fr model.FlowResult) model.Result {
	res := model.Result{
		CaseID:           fr.FlowID,
		OperationID:      "",
		Passed:           fr.Passed,
		StrategyKind:     string(strategy.KindStateful),
		StatefulResult:   &fr,
		SchemaViolations: []model.SchemaViolation{},
		Failures:         flowResultFailures(fr),
	}
	if len(fr.Steps) > 0 {
		res.StatusCode = fr.Steps[len(fr.Steps)-1].StatusCode
		res.Request = resolvedToRequestDetail(fr.Steps[0].Request)
		res.Response = stepResponseToDetail(fr.Steps[len(fr.Steps)-1].Response)
	}
	if !fr.Passed {
		for _, st := range fr.Steps {
			if !st.Passed {
				res.OperationID = st.OperationID
				break
			}
		}
	}
	res.ArtifactPath = fr.ArtifactPath
	return res
}

func flowResultFailures(fr model.FlowResult) []model.Failure {
	return artifactFailures(fr.Steps)
}
