package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/jimbery/bt/internal/adapter/openapi"
	"github.com/jimbery/bt/internal/mcp/registry"
	"github.com/jimbery/bt/pkg/model"
)

const descDiscoverOperations = `bt_discover_operations parses an OpenAPI schema file and returns all discovered operations as a structured list. Use bt_discover_operations before bt_suggest_strategy or bt_scaffold_config.`

// DiscoverOperationsHandler implements the bt_discover_operations MCP tool.
func DiscoverOperationsHandler() registry.HandlerFunc {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			SchemaPath string `json:"schema_path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("decode input: %w", err)
		}
		if strings.TrimSpace(in.SchemaPath) == "" {
			return nil, fmt.Errorf("schema_path is required")
		}
		abs := in.SchemaPath
		if a, absErr := filepath.Abs(in.SchemaPath); absErr == nil {
			abs = a
		}
		ad := openapi.New()
		ops, discErr := ad.Discover(ctx, model.Target{SchemaPath: abs})
		if discErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": discErr.Error(),
				"code":  "SCHEMA_PARSE_ERROR",
			})
			return out, nil
		}
		summaries := make([]map[string]any, 0, len(ops))
		for _, op := range ops {
			rc := make([]int, 0, len(op.Responses))
			for _, r := range op.Responses {
				rc = append(rc, r.StatusCode)
			}
			summary := ""
			if len(op.Tags) > 0 {
				summary = strings.Join(op.Tags, ", ")
			}
			summaries = append(summaries, map[string]any{
				"id":             op.ID,
				"method":         op.Method,
				"path":           op.Path,
				"summary":        summary,
				"has_body":       op.RequestBody != nil,
				"param_count":    len(op.Parameters),
				"response_codes": rc,
			})
		}
		out, err := json.Marshal(map[string]any{
			"operations":      summaries,
			"operation_count": len(summaries),
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}
