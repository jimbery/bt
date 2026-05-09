package table

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/pkg/model"
)

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
}

type caseExpectedEntry struct {
	StatusCode int               `yaml:"status_code"`
	Headers    map[string]string `yaml:"headers"`
}

// Strategy implements strategy.Strategy for table-driven testing.
type Strategy struct{}

// New returns a new table Strategy.
func New() strategy.Strategy {
	return &Strategy{}
}

func (s *Strategy) Name() strategy.Kind { return strategy.KindTable }

// Plan loads cases from the YAML file specified in spec.Config["file"].
// It does not make network calls.
func (s *Strategy) Plan(_ context.Context, spec strategy.Spec, _ []model.Operation) ([]model.Case, error) {
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
				Method:  entry.Input.Method,
				Path:    entry.Input.Path,
				Headers: entry.Input.Headers,
				Query:   entry.Input.Query,
				Body:    entry.Input.Body,
			},
		}
		if entry.Expected != nil {
			c.Expected = &model.CaseExpectation{
				StatusCode: entry.Expected.StatusCode,
				Headers:    entry.Expected.Headers,
			}
		}
		cases = append(cases, c)
	}

	return cases, nil
}

// Execute runs each case through the executor and evaluates assertions.
func (s *Strategy) Execute(ctx context.Context, cases []model.Case, exec strategy.Executor) ([]model.Result, error) {
	results := make([]model.Result, 0, len(cases))

	for _, c := range cases {
		start := time.Now()
		resp, err := exec.Run(ctx, c.Input)
		dur := time.Since(start)
		if err != nil {
			return nil, fmt.Errorf("case %q: executor error: %w", c.ID, err)
		}

		result := model.Result{
			CaseID:     c.ID,
			StatusCode: resp.StatusCode,
			Duration:   dur,
			Response:   resp,
			Request: model.RequestDetail{
				Method:  c.Input.Method,
				URL:     "",
				Headers: c.Input.Headers,
			},
		}

		var failures []model.Failure

		if c.Expected != nil {
			if c.Expected.StatusCode != 0 && resp.StatusCode != c.Expected.StatusCode {
				failures = append(failures, model.Failure{
					Invariant: "status_code",
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
						Invariant: "response_header",
						Message:   fmt.Sprintf("header %q: expected %q, got %q", header, want, got),
						Expected:  want,
						Actual:    got,
					})
				}
			}
		}

		result.Failures = failures
		result.Passed = len(failures) == 0
		results = append(results, result)
	}

	return results, nil
}
