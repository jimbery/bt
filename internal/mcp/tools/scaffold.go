package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/jayimbery/bt/internal/adapter/openapi"
	"github.com/jayimbery/bt/internal/mcp/registry"
	"github.com/jayimbery/bt/pkg/model"
)

const descScaffoldConfig = `bt_scaffold_config builds a starter backendtest.yaml from an OpenAPI schema and optional strategy list; it returns config_yaml text. After bt_scaffold_config, use bt_validate then bt_run. You may pass output_path so bt_scaffold_config writes the file and cases for you.`

// ScaffoldConfigHandler implements the bt_scaffold_config MCP tool.
func ScaffoldConfigHandler() registry.HandlerFunc {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			SchemaPath string   `json:"schema_path"`
			BaseURL    string   `json:"base_url"`
			Strategies []string `json:"strategies"`
			OutputPath string   `json:"output_path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("decode input: %w", err)
		}
		if strings.TrimSpace(in.SchemaPath) == "" {
			return nil, fmt.Errorf("schema_path is required")
		}
		absSchema := in.SchemaPath
		if a, absErr := filepath.Abs(in.SchemaPath); absErr == nil {
			absSchema = a
		}
		baseURL := strings.TrimSpace(in.BaseURL)
		if baseURL == "" {
			baseURL = "http://localhost:8080"
		}
		strategies := in.Strategies
		if len(strategies) == 0 {
			strategies = []string{"table"}
		}
		want := map[string]bool{}
		for _, s := range strategies {
			want[strings.ToLower(strings.TrimSpace(s))] = true
		}

		ad := openapi.New()
		ops, discErr := ad.Discover(ctx, model.Target{SchemaPath: absSchema})
		if discErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": discErr.Error(),
				"code":  "SCHEMA_PARSE_ERROR",
			})
			return out, nil
		}

		targetName := strings.TrimSuffix(filepath.Base(absSchema), filepath.Ext(absSchema)) + "-api"
		if targetName == "-api" {
			targetName = "scaffolded-api"
		}

		var casesRel string
		var casesAbs string
		if strings.TrimSpace(in.OutputPath) != "" {
			outAbs, err := filepath.Abs(in.OutputPath)
			if err != nil {
				outAbs = in.OutputPath
			}
			cfgDir := filepath.Dir(outAbs)
			if err := os.MkdirAll(filepath.Join(cfgDir, "cases"), 0o755); err != nil {
				return nil, fmt.Errorf("mkdir cases: %w", err)
			}
			casesRel = "cases/table.yaml"
			casesAbs = filepath.Join(cfgDir, casesRel)
		} else {
			f, err := os.CreateTemp("", "bt-scaffold-cases-*.yaml")
			if err != nil {
				return nil, fmt.Errorf("temp cases file: %w", err)
			}
			casesAbs = f.Name()
			_ = f.Close()
			casesRel = casesAbs
		}

		casesBody := renderCasesYAML(ops)
		if err := os.WriteFile(casesAbs, []byte(casesBody), 0o644); err != nil {
			return nil, fmt.Errorf("write cases: %w", err)
		}

		strategyYAML := buildStrategyYAML(casesRel, want)

		root := map[string]any{
			"version": 1,
			"target": map[string]any{
				"name":     targetName,
				"base_url": baseURL,
				"schema":   absSchema,
			},
			"strategies": strategyYAML,
			"report": map[string]any{
				"formats":    []string{"console", "json"},
				"output_dir": ".bt/reports",
			},
			"safety": map[string]any{
				"profile": "safe",
			},
		}

		yamlBytes, err := yaml.Marshal(root)
		if err != nil {
			return nil, err
		}
		configYAML := string(yamlBytes)

		written := false
		outPath := ""
		if p := strings.TrimSpace(in.OutputPath); p != "" {
			outAbs := p
			if a, absErr := filepath.Abs(p); absErr == nil {
				outAbs = a
			}
			if err := os.MkdirAll(filepath.Dir(outAbs), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(outAbs, yamlBytes, 0o644); err != nil {
				return nil, err
			}
			written = true
			outPath = outAbs
		}

		resp := map[string]any{
			"config_yaml":     configYAML,
			"written_to_disk": written,
		}
		if outPath != "" {
			resp["output_path"] = outPath
		}
		return json.Marshal(resp)
	}
}

func renderCasesYAML(ops []model.Operation) string {
	if len(ops) == 0 {
		return "cases: []\n"
	}
	op := ops[0]
	code := 200
	for _, r := range op.Responses {
		if r.StatusCode >= 200 && r.StatusCode < 300 {
			code = r.StatusCode
			break
		}
	}
	return fmt.Sprintf(`cases:
  - id: smoke-%s
    operation_id: %s
    input:
      method: %s
      path: %s
    expected:
      status_code: %d
`, op.ID, op.ID, op.Method, op.Path, code)
}

func buildStrategyYAML(casesFile string, want map[string]bool) []any {
	var out []any
	out = append(out, map[string]any{
		"type": "table",
		"file": casesFile,
	})
	if want["property"] {
		out = append(out, map[string]any{
			"type":       "property",
			"operations": []string{},
			"invariants": []string{"no_5xx", "response_matches_schema"},
			"config": map[string]any{
				"checks": 30,
			},
		})
	}
	if want["fuzz"] {
		out = append(out, map[string]any{
			"type":       "fuzz",
			"operations": []string{},
			"config": map[string]any{
				"fuzz_iterations": 50,
			},
		})
	}
	return out
}
