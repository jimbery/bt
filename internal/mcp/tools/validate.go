package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/mcp/registry"
)

const descValidate = `bt_validate checks a backendtest.yaml file and returns valid plus a list of errors. Run bt_validate before bt_run so configuration issues are caught early.`

// ValidateHandler implements the bt_validate MCP tool.
func ValidateHandler() registry.HandlerFunc {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		_ = ctx
		var in struct {
			ConfigPath string `json:"config_path"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("decode input: %w", err)
		}
		if strings.TrimSpace(in.ConfigPath) == "" {
			return nil, fmt.Errorf("config_path is required")
		}
		_, loadErr := config.Load(in.ConfigPath)
		if loadErr != nil {
			if errors.Is(loadErr, config.ErrConfigNotFound) {
				out, _ := json.Marshal(map[string]any{
					"valid": false,
					"errors": []map[string]string{
						{"field": "config_path", "message": "config file not found"},
					},
				})
				return out, nil
			}
			out, _ := json.Marshal(map[string]any{
				"valid": false,
				"errors": []map[string]string{
					{"field": "config_path", "message": loadErr.Error()},
				},
			})
			return out, nil
		}
		out, marshalErr := json.Marshal(map[string]any{
			"valid":  true,
			"errors": []any{},
		})
		if marshalErr != nil {
			return nil, marshalErr
		}
		return out, nil
	}
}
