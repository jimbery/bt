package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jimbery/bt/internal/mcp/registry"
	"github.com/jimbery/bt/pkg/model"
)

const descExplainFailure = `bt_explain_failure loads one bt artifact JSON and returns request, response, failures, and a replay_command line. Use bt_explain_failure after bt_run when you need full detail for a single artifact_path.`

// ExplainFailureHandler implements the bt_explain_failure MCP tool.
func ExplainFailureHandler() registry.HandlerFunc {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		_ = ctx
		var in struct {
			ArtifactPath string `json:"artifact_path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("decode input: %w", err)
		}
		if strings.TrimSpace(in.ArtifactPath) == "" {
			return nil, fmt.Errorf("artifact_path is required")
		}
		abs := in.ArtifactPath
		if a, absErr := filepath.Abs(in.ArtifactPath); absErr == nil {
			abs = a
		}
		data, readErr := os.ReadFile(abs)
		if readErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": "artifact not found",
				"code":  "ARTIFACT_NOT_FOUND",
				"path":  abs,
			})
			return out, nil
		}
		var art model.Artifact
		if err := json.Unmarshal(data, &art); err != nil {
			return nil, fmt.Errorf("parse artifact: %w", err)
		}

		cfgGuess := inferConfigPath(abs)
		replayCmd := fmt.Sprintf("bt replay --config %q %q", cfgGuess, abs)

		failures := make([]map[string]any, 0, len(art.Failures))
		for _, f := range art.Failures {
			entry := map[string]any{
				"invariant": f.Invariant,
				"message":   f.Message,
			}
			if f.Path != "" {
				entry["path"] = f.Path
			}
			if f.Expected != nil {
				entry["expected"] = fmt.Sprint(f.Expected)
			}
			if f.Actual != nil {
				entry["actual"] = fmt.Sprint(f.Actual)
			}
			if f.Classification != "" {
				entry["classification"] = f.Classification
			}
			failures = append(failures, entry)
		}

		out := map[string]any{
			"case_id":     art.CaseID,
			"strategy":    art.StrategyKind,
			"occurred_at": art.OccurredAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			"request": map[string]any{
				"method":  art.Request.Method,
				"url":     art.Request.URL,
				"headers": headersOrEmpty(art.Request.Headers),
				"body":    string(art.Request.Body),
			},
			"response": map[string]any{
				"status_code": art.Response.StatusCode,
				"headers":     headersOrEmpty(art.Response.Headers),
				"body":        string(art.Response.Body),
			},
			"failures":       failures,
			"replay_command": replayCmd,
		}
		if art.Seed != 0 {
			out["seed"] = art.Seed
		}
		return json.Marshal(out)
	}
}

func headersOrEmpty(h map[string]string) map[string]any {
	if len(h) == 0 {
		return map[string]any{}
	}
	m := make(map[string]any, len(h))
	for k, v := range h {
		m[k] = v
	}
	return m
}

func inferConfigPath(artifactPath string) string {
	dir := filepath.Dir(artifactPath)
	// .../something/.bt/artifacts/file.json -> walk up to find backendtest.yaml
	for i := 0; i < 6; i++ {
		cand := filepath.Join(dir, "backendtest.yaml")
		if st, err := os.Stat(cand); err == nil && !st.IsDir() {
			return cand
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "backendtest.yaml"
}
