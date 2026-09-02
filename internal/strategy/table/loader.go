package table

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jimbery/bt/internal/adapter/openapi"
	"github.com/jimbery/bt/internal/strategy"
	"github.com/jimbery/bt/pkg/model"
)

// ConfigError is returned when a table case file references an invalid OpenAPI fragment.
type ConfigError struct{ msg string }

func (e *ConfigError) Error() string { return e.msg }

// IsConfigError reports whether err is a *ConfigError.
func IsConfigError(err error) bool {
	var ce *ConfigError
	return errors.As(err, &ce)
}

// LoadCasesFromReader parses table cases from YAML. Component $ref values are not resolved.
func LoadCasesFromReader(r io.Reader) ([]model.Case, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return casesFromYAMLBytes(data, nil)
}

// LoadCasesWithSpec parses table cases and resolves schema $ref values against the OpenAPI document bytes.
func LoadCasesWithSpec(r io.Reader, openAPIDoc []byte) ([]model.Case, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return casesFromYAMLBytes(data, openAPIDoc)
}

func casesFromYAMLBytes(data []byte, openAPIDoc []byte) ([]model.Case, error) {
	var cf caseFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("cannot parse case yaml: %w", err)
	}
	cases, err := modelCasesFromCaseFile(cf)
	if err != nil {
		return nil, err
	}
	if len(openAPIDoc) > 0 {
		if err := resolveSchemaRefsInCases(cases, openAPIDoc); err != nil {
			return nil, err
		}
	}
	return cases, nil
}

func modelCasesFromCaseFile(cf caseFile) ([]model.Case, error) {
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

func resolveSchemaRefsInCases(cases []model.Case, openAPI []byte) error {
	for i := range cases {
		c := &cases[i]
		if c.Expected == nil {
			continue
		}
		if c.Expected.Schema != nil && strings.TrimSpace(c.Expected.Schema.Ref) != "" {
			resolved, err := openapi.ResolveComponentSchema(openAPI, c.Expected.Schema.Ref)
			if err != nil {
				return &ConfigError{msg: fmt.Sprintf("case %q: expected.schema: %v", c.ID, err)}
			}
			c.Expected.Schema = resolved
		}
	}
	return nil
}

func readOptionalOpenAPI(spec strategy.Spec) ([]byte, error) {
	if spec.Config == nil {
		return nil, nil
	}
	raw, ok := spec.Config["target_schema_path"].(string)
	if !ok {
		return nil, nil
	}
	p := strings.TrimSpace(raw)
	if p == "" {
		return nil, nil
	}
	return os.ReadFile(p)
}
