package table

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jayimbery/bt/internal/strategy"
	gqlassert "github.com/jayimbery/bt/internal/strategy/graphql/assert"
	"github.com/jayimbery/bt/internal/strategy/property/validate"
	"github.com/jayimbery/bt/pkg/model"
)

// ArtifactWriter is the interface the table strategy uses to write failure artifacts.
// replay.Writer satisfies this interface.
type ArtifactWriter interface {
	Write(a model.Artifact) (string, error)
}

// Options configures optional behaviour for the table strategy.
type Options struct {
	// ArtifactWriter is called on each failing case to write a replay bundle.
	// If nil, no artifacts are written.
	ArtifactWriter ArtifactWriter

	// Environment is recorded in artifact bundles for context.
	Environment string

	// GQLExecutor runs GraphQL cases when CaseInput.IsGraphQL() is true.
	// If nil, GraphQL cases fail with a clear configuration error.
	GQLExecutor strategy.Executor

	// DefaultHeaders are merged into each request before execution (for example
	// Authorization from target.auth). Per-case input.headers override the same key.
	DefaultHeaders map[string]string

	// AuthDebugEnvName is the name of the process env var from target.auth.env
	// (when set). Used only for failure artifacts so you can see whether that var
	// was non-empty when bt ran — never stores the secret value.
	AuthDebugEnvName string
}

type tableStrategy struct {
	opts Options
}

// New returns a table Strategy with default options (no artifact writing).
func New() strategy.Strategy {
	return &tableStrategy{}
}

// NewWithOptions returns a table Strategy with the given options.
func NewWithOptions(opts Options) strategy.Strategy {
	return &tableStrategy{opts: opts}
}

func (s *tableStrategy) Name() strategy.Kind { return strategy.KindTable }

// caseFile is the on-disk format for a table test case file.
type caseFile struct {
	Cases []caseEntry `yaml:"cases"`
}

type caseEntry struct {
	ID          string             `yaml:"id"`
	OperationID string             `yaml:"operation_id"`
	Input       caseInputEntry     `yaml:"input"`
	Expected    *caseExpectedEntry `yaml:"expected"`
}

type caseInputEntry struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
	Query   map[string]string `yaml:"query"`
	Body    any               `yaml:"body"`

	GQLQuery         string         `yaml:"gql_query,omitempty"`
	GQLOperationName string         `yaml:"gql_operation_name,omitempty"`
	GQLVariables     map[string]any `yaml:"gql_variables,omitempty"`
}

type caseExpectedEntry struct {
	StatusCode int               `yaml:"status_code"`
	Headers    map[string]string `yaml:"headers"`
	Schema     any               `yaml:"schema,omitempty"`

	GQLData       map[string]any `yaml:"gql_data,omitempty"`
	GQLNoErrors   *bool          `yaml:"gql_no_errors,omitempty"`
	GQLHasErrors  *bool          `yaml:"gql_has_errors,omitempty"`
	GQLDataSchema any            `yaml:"gql_data_schema,omitempty"`
}

// Plan loads cases from the YAML file specified in spec.Config["file"].
// It does not make network calls.
func (s *tableStrategy) Plan(_ context.Context, spec strategy.Spec, _ []model.Operation) ([]model.Case, error) {
	filePath, ok := spec.Config["file"].(string)
	if !ok || filePath == "" {
		return nil, errors.New("table strategy requires config.file to be set")
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("cannot read case file: %w", err)
	}

	var cf caseFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("cannot parse case file: %w", err)
	}

	cases := make([]model.Case, 0, len(cf.Cases))
	for _, entry := range cf.Cases {
		c := model.Case{
			ID:          entry.ID,
			OperationID: entry.OperationID,
			Input: model.CaseInput{
				Method:           entry.Input.Method,
				Path:             entry.Input.Path,
				Headers:          entry.Input.Headers,
				Query:            entry.Input.Query,
				Body:             entry.Input.Body,
				GQLQuery:         entry.Input.GQLQuery,
				GQLOperationName: entry.Input.GQLOperationName,
				GQLVariables:     entry.Input.GQLVariables,
			},
		}
		if entry.Expected != nil {
			exp := &model.CaseExpectation{
				StatusCode: entry.Expected.StatusCode,
				Headers:    entry.Expected.Headers,
			}
			if entry.Expected.Schema != nil {
				sch, err := schemaRefFromYAML(entry.Expected.Schema)
				if err != nil {
					return nil, fmt.Errorf("case %q: expected.schema: %w", entry.ID, err)
				}
				exp.Schema = sch
			}
			if len(entry.Expected.GQLData) > 0 {
				exp.GQLData = entry.Expected.GQLData
			}
			if entry.Expected.GQLNoErrors != nil {
				exp.GQLNoErrors = entry.Expected.GQLNoErrors
			}
			if entry.Expected.GQLHasErrors != nil {
				exp.GQLHasErrors = entry.Expected.GQLHasErrors
			}
			if entry.Expected.GQLDataSchema != nil {
				sch, err := schemaRefFromYAML(entry.Expected.GQLDataSchema)
				if err != nil {
					return nil, fmt.Errorf("case %q: expected.gql_data_schema: %w", entry.ID, err)
				}
				exp.GQLDataSchema = sch
			}
			c.Expected = exp
		}
		cases = append(cases, c)
	}

	return cases, nil
}

func schemaRefFromYAML(v any) (*model.SchemaRef, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var sch model.SchemaRef
	if err := json.Unmarshal(raw, &sch); err != nil {
		return nil, err
	}
	return &sch, nil
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

func mergeCaseInputHeaders(in model.CaseInput, defaults map[string]string) model.CaseInput {
	if len(defaults) == 0 {
		return in
	}
	out := in
	merged := make(map[string]string, len(defaults)+len(in.Headers))
	for k, v := range defaults {
		merged[http.CanonicalHeaderKey(k)] = v
	}
	for k, v := range in.Headers {
		merged[http.CanonicalHeaderKey(k)] = v
	}
	out.Headers = merged
	return out
}

func requestDetailFromInput(in model.CaseInput) model.RequestDetail {
	rd := model.RequestDetail{
		Method:  in.Method,
		URL:     in.Path,
		Headers: cloneStringMap(in.Headers),
		Query:   cloneStringMap(in.Query),
	}
	if in.IsGraphQL() {
		m := map[string]any{"query": in.GQLQuery}
		if strings.TrimSpace(in.GQLOperationName) != "" {
			m["operationName"] = in.GQLOperationName
		}
		if in.GQLVariables != nil {
			m["variables"] = in.GQLVariables
		}
		if b, err := json.Marshal(m); err == nil {
			rd.Body = b
		}
	} else if in.Body != nil {
		if b, err := json.Marshal(in.Body); err == nil {
			rd.Body = b
		}
	}
	return rd
}

func redactAuthorizationHeader(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(v), "bearer ") {
		return "Bearer <redacted>"
	}
	return "<redacted>"
}

func artifactRequestSnapshot(rd model.RequestDetail) model.RequestDetail {
	if len(rd.Headers) == 0 {
		return rd
	}
	out := rd
	out.Headers = cloneStringMap(rd.Headers)
	for k, v := range out.Headers {
		if http.CanonicalHeaderKey(k) != "Authorization" || v == "" {
			continue
		}
		out.Headers[k] = redactAuthorizationHeader(v)
	}
	return out
}

// Execute runs each case through the executor and evaluates assertions.
func (s *tableStrategy) Execute(ctx context.Context, cases []model.Case, exec strategy.Executor) ([]model.Result, error) {
	results := make([]model.Result, 0, len(cases))

	for _, c := range cases {
		execInput := mergeCaseInputHeaders(c.Input, s.opts.DefaultHeaders)

		if c.Input.IsGraphQL() && c.ResolvedOperation != nil && c.ResolvedOperation.GQLKind == model.GQLSubscription {
			failures := []model.Failure{{
				Invariant: model.InvariantGraphQLResponse,
				Message:   "GraphQL subscriptions are discovered but not executed by bt",
			}}
			results = append(results, model.Result{
				CaseID:       c.ID,
				Passed:       false,
				StrategyKind: string(strategy.KindTable),
				Request:      requestDetailFromInput(execInput),
				Response:     model.ResponseDetail{},
				Failures:     failures,
			})
			continue
		}

		start := time.Now()
		var resp model.ResponseDetail
		var err error
		if c.Input.IsGraphQL() {
			if s.opts.GQLExecutor != nil {
				resp, err = s.opts.GQLExecutor.Run(ctx, execInput)
			} else {
				resp, err = exec.Run(ctx, execInput)
			}
		} else {
			resp, err = exec.Run(ctx, execInput)
		}
		dur := time.Since(start)
		if err != nil {
			return nil, fmt.Errorf("case %q: executor error: %w", c.ID, err)
		}

		result := model.Result{
			CaseID:       c.ID,
			StatusCode:   resp.StatusCode,
			Duration:     dur,
			Response:     resp,
			Request:      requestDetailFromInput(execInput),
			StrategyKind: string(strategy.KindTable),
		}

		var failures []model.Failure

		if c.Expected != nil {
			if c.Expected.StatusCode != 0 && resp.StatusCode != c.Expected.StatusCode {
				failures = append(failures, model.Failure{
					Invariant: model.InvariantStatusCode,
					Message:   fmt.Sprintf("expected status %d, got %d", c.Expected.StatusCode, resp.StatusCode),
					Expected:  c.Expected.StatusCode,
					Actual:    resp.StatusCode,
				})
			}

			for header, want := range c.Expected.Headers {
				key := http.CanonicalHeaderKey(header)
				got := resp.Headers[key]
				if got != want {
					failures = append(failures, model.Failure{
						Invariant: model.InvariantResponseHeader,
						Message:   fmt.Sprintf("header %q: expected %q, got %q", header, want, got),
						Expected:  want,
						Actual:    got,
					})
				}
			}

			if c.Expected.Schema != nil {
				for _, v := range validate.ValidateResponse(resp.Body, c.Expected.Schema) {
					failures = append(failures, model.Failure{
						Invariant: model.InvariantResponseMatchesSchema,
						Message:   v.Message,
						Path:      v.Path,
						Expected:  v.Expected,
						Actual:    v.Got,
					})
				}
			}
		}

		if c.Input.IsGraphQL() && c.ResolvedOperation != nil {
			skipGQLAssert := c.Expected != nil && c.Expected.GQLHasErrors != nil && *c.Expected.GQLHasErrors
			if !skipGQLAssert {
				// When the case supplies gql_data_schema, shape is asserted there against the
				// actual response. Skip the adapter-derived GQLSelectionSchema check — it reflects
				// the full GraphQL return type from SDL, while table cases often send a narrower
				// gql_query than the discovered minimal document.
				opAssert := *c.ResolvedOperation
				if c.Expected != nil && c.Expected.GQLDataSchema != nil {
					opAssert.GQLSelectionSchema = nil
				}
				for _, g := range gqlassert.AssertResponse(resp.Body, opAssert) {
					class := ""
					if g.Severity == gqlassert.Warning {
						class = "graphql_warning"
					}
					failures = append(failures, model.Failure{
						Invariant:      model.InvariantGraphQLResponse,
						Classification: class,
						Message:        g.Message,
						Path:           g.Field,
					})
				}
			}
		}

		if c.Input.IsGraphQL() {
			failures = append(failures, gqlTableExpectationFailures(resp.Body, c.Expected)...)
		}

		result.Failures = failures
		result.Passed = !tableCaseHasBlockingFailure(failures)

		if !result.Passed && s.opts.ArtifactWriter != nil {
			artifact := model.Artifact{
				ID:           fmt.Sprintf("%s-%d", c.ID, time.Now().UnixNano()),
				StrategyKind: string(strategy.KindTable),
				CaseID:       c.ID,
				OccurredAt:   time.Now().UTC(),
				Environment:  s.opts.Environment,
				Request:      artifactRequestSnapshot(result.Request),
				Response:     resp,
				Failures:     failures,
				Expected:     c.Expected,
			}
			if env := strings.TrimSpace(s.opts.AuthDebugEnvName); env != "" {
				artifact.AuthEnvName = env
				artifact.AuthEnvSetInProcess = strings.TrimSpace(os.Getenv(env)) != ""
			}
			artifactPath, writeErr := s.opts.ArtifactWriter.Write(artifact)
			if writeErr != nil {
				_, _ = fmt.Fprintf(os.Stderr, "warning: could not write artifact for case %q: %v\n", c.ID, writeErr)
			} else {
				result.ArtifactPath = artifactPath
			}
		}

		results = append(results, result)
	}

	return results, nil
}

func tableCaseHasBlockingFailure(failures []model.Failure) bool {
	for _, f := range failures {
		if f.Invariant == model.InvariantGraphQLResponse && f.Classification == "graphql_warning" {
			continue
		}
		return true
	}
	return false
}
