// Package loader parses and validates stateful flow YAML (M13).
package loader

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/jayimbery/bt/internal/strategy/stateful/binding"
	"github.com/jayimbery/bt/pkg/model"
)

var placeholderRe = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

// IsConfigError reports load-time configuration failures.
func IsConfigError(err error) bool { return errors.Is(err, errConfig) }

var errConfig = errors.New("stateful loader: config error")

func wrapConfig(msg string) error {
	return fmt.Errorf("%w: %s", errConfig, msg)
}

type flowFile struct {
	Flows []flowYAML `yaml:"flows"`
}

type flowYAML struct {
	ID          string     `yaml:"id"`
	Description string     `yaml:"description"`
	Steps       []stepYAML `yaml:"steps"`
}

type stepYAML struct {
	ID          string                 `yaml:"id"`
	OperationID string                 `yaml:"operation_id"`
	Input       stepInputYAML          `yaml:"input"`
	Expected    *expectedYAML          `yaml:"expected"`
	Extract     map[string]extractYAML `yaml:"extract"`
}

type stepInputYAML struct {
	Method  string            `yaml:"method"`
	Path    string            `yaml:"path"`
	Headers map[string]string `yaml:"headers"`
	Query   map[string]string `yaml:"query"`
	Body    any               `yaml:"body"`
}

type expectedYAML struct {
	StatusCode int            `yaml:"status_code"`
	Schema     map[string]any `yaml:"schema"`
}

type extractYAML struct {
	From string `yaml:"from"`
	Into string `yaml:"into"`
}

// LoadFlow parses YAML and returns the first flow in the document.
func LoadFlow(r io.Reader) (*model.Flow, error) {
	flows, err := loadAllFlows(r)
	if err != nil {
		return nil, err
	}
	if len(flows) == 0 {
		return nil, wrapConfig("no flows defined")
	}
	return &flows[0], nil
}

// LoadFlowFile reads a YAML file and returns the first flow.
func LoadFlowFile(path string) (*model.Flow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return LoadFlow(f)
}

// LoadFlowsFile reads a YAML file and returns every flow in the document.
func LoadFlowsFile(path string) ([]model.Flow, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadAllFlows(bytes.NewReader(data))
}

// LoadFlowsDir loads every *.yaml and *.yml in dir (sorted by name).
func LoadFlowsDir(dir string) ([]model.Flow, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		low := strings.ToLower(n)
		if strings.HasSuffix(low, ".yaml") || strings.HasSuffix(low, ".yml") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	var out []model.Flow
	for _, n := range names {
		path := filepath.Join(dir, n)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		flows, err := loadAllFlows(strings.NewReader(string(data)))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, flows...)
	}
	return out, nil
}

func loadAllFlows(r io.Reader) ([]model.Flow, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var doc flowFile
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	var out []model.Flow
	for _, fy := range doc.Flows {
		f, err := convertAndValidateFlow(fy)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, nil
}

func convertAndValidateFlow(fy flowYAML) (model.Flow, error) {
	if strings.TrimSpace(fy.ID) == "" {
		return model.Flow{}, wrapConfig("flow missing id")
	}
	f := model.Flow{
		ID:          fy.ID,
		Description: fy.Description,
		Steps:       make([]model.FlowStep, 0, len(fy.Steps)),
	}
	accum := map[string]struct{}{}
	for _, sy := range fy.Steps {
		if err := validateStepRefs(sy, accum); err != nil {
			return model.Flow{}, err
		}
		step, err := convertStep(sy)
		if err != nil {
			return model.Flow{}, err
		}
		if err := pass1Extract(step); err != nil {
			return model.Flow{}, err
		}
		f.Steps = append(f.Steps, step)
		for k := range step.Extract {
			accum[k] = struct{}{}
		}
	}
	return f, nil
}

func pass1Extract(step model.FlowStep) error {
	for key, spec := range step.Extract {
		if err := binding.ValidateExpression(spec.From); err != nil {
			return wrapConfig(fmt.Sprintf("extract %q from %q: %v", key, spec.From, err))
		}
		if strings.TrimSpace(spec.From) == "$" && !strings.EqualFold(strings.TrimSpace(spec.Into), "body") {
			return wrapConfig(fmt.Sprintf("extract %q: $ may only target into body, got %q", key, spec.Into))
		}
	}
	return nil
}

func validateStepRefs(sy stepYAML, accum map[string]struct{}) error {
	check := func(s string) error {
		for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
			if len(m) < 2 {
				continue
			}
			k := m[1]
			if _, ok := accum[k]; !ok {
				return wrapConfig(fmt.Sprintf("undefined binding placeholder {%s}", k))
			}
		}
		return nil
	}
	if err := check(sy.Input.Path); err != nil {
		return err
	}
	for _, v := range sy.Input.Query {
		if err := check(v); err != nil {
			return err
		}
	}
	for _, v := range sy.Input.Headers {
		if err := check(v); err != nil {
			return err
		}
	}
	return nil
}

func convertStep(sy stepYAML) (model.FlowStep, error) {
	st := model.FlowStep{
		ID:          sy.ID,
		OperationID: sy.OperationID,
		Input: model.StepInput{
			Method:  sy.Input.Method,
			Path:    sy.Input.Path,
			Headers: sy.Input.Headers,
			Query:   sy.Input.Query,
			Body:    sy.Input.Body,
		},
	}
	if sy.Expected != nil {
		exp := &model.StepExpectation{StatusCode: sy.Expected.StatusCode}
		if sy.Expected.Schema != nil {
			sr, err := schemaRefFromMap(sy.Expected.Schema)
			if err != nil {
				return model.FlowStep{}, err
			}
			exp.Schema = sr
		}
		st.Expected = exp
	}
	if len(sy.Extract) > 0 {
		st.Extract = make(map[string]model.ExtractSpec)
		for k, v := range sy.Extract {
			st.Extract[k] = model.ExtractSpec{From: v.From, Into: v.Into}
		}
	}
	return st, nil
}

func schemaRefFromMap(m map[string]any) (*model.SchemaRef, error) {
	if m == nil {
		return nil, nil
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var s model.SchemaRef
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
