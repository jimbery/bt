package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/mcp/registry"
	"github.com/jayimbery/bt/internal/runplan"
	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/pkg/model"
)

const descRun = `bt_run executes a configured bt strategy (table, property, fuzz, contract, or all) and returns passed, failed, and artifact paths. After bt_run reports failures, use bt_explain_failure with the artifact_path for full detail.`

// RunHandler implements the bt_run MCP tool.
func RunHandler() registry.HandlerFunc {
	return func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
		var in struct {
			ConfigPath string `json:"config_path"`
			Strategy   string `json:"strategy"`
			Seed       *int64 `json:"seed"`
		}
		if err := json.Unmarshal(input, &in); err != nil {
			return nil, fmt.Errorf("decode input: %w", err)
		}
		if strings.TrimSpace(in.ConfigPath) == "" {
			return nil, fmt.Errorf("config_path is required")
		}
		cfgPath := in.ConfigPath
		cfg, loadErr := config.Load(cfgPath)
		if loadErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": loadErr.Error(),
				"code":  "CONFIG_ERROR",
			})
			return out, nil
		}
		strategyName := strings.TrimSpace(in.Strategy)
		if strategyName == "" {
			if len(cfg.Strategies) > 0 {
				strategyName = cfg.Strategies[0].Type
			} else {
				strategyName = "table"
			}
		}

		ad := runplan.AdapterForName(cfg.Target.Adapter)
		target := cfg.Target.AsModel()
		if valErr := ad.Validate(ctx, target); valErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": fmt.Sprintf("adapter validate: %v", valErr),
				"code":  "CONFIG_ERROR",
			})
			return out, nil
		}
		ops, discErr := ad.Discover(ctx, target)
		if discErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": fmt.Sprintf("adapter discover: %v", discErr),
				"code":  "CONFIG_ERROR",
			})
			return out, nil
		}

		opt := runplan.BuildOptions{Stderr: nil}
		if in.Seed != nil {
			opt.SeedProvided = true
			opt.Seed = *in.Seed
		}

		st, spec, buildErr := runplan.BuildStrategyAndSpec(cfgPath, strategyName, cfg, opt)
		if buildErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": buildErr.Error(),
				"code":  "CONFIG_ERROR",
			})
			return out, nil
		}

		cases, planErr := st.Plan(ctx, spec, ops)
		if planErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": planErr.Error(),
				"code":  "PLAN_ERROR",
			})
			return out, nil
		}
		runplan.AttachResolvedOperations(cases, ops)

		exec := runplan.BuildDefaultExecutor(cfg, cfg.Target.Adapter)

		start := time.Now()
		results, execErr := st.Execute(ctx, cases, exec)
		durMs := int(time.Since(start).Milliseconds())
		if execErr != nil {
			out, _ := json.Marshal(map[string]any{
				"error": execErr.Error(),
				"code":  "EXEC_ERROR",
			})
			return out, nil
		}

		artifactDir := filepath.Join(filepath.Dir(cfgPath), ".bt", "artifacts")
		passed, failed, skipped := 0, 0, 0
		failures := make([]map[string]any, 0)
		for _, res := range results {
			switch {
			case res.Skipped:
				skipped++
			case res.Passed || res.Quarantined:
				passed++
			default:
				failed++
				summary := failureSummary(res)
				failures = append(failures, map[string]any{
					"case_id":       res.CaseID,
					"artifact_path": res.ArtifactPath,
					"summary":       summary,
				})
			}
		}
		total := len(results)

		out := map[string]any{
			"passed":       passed,
			"failed":       failed,
			"skipped":      skipped,
			"total":        total,
			"strategy":     strategyName,
			"duration_ms":  durMs,
			"artifact_dir": artifactDir,
			"failures":     failures,
		}
		return json.Marshal(out)
	}
}

func failureSummary(res model.Result) string {
	if len(res.Failures) == 0 {
		if strategy.Kind(res.StrategyKind) == strategy.KindFuzz {
			return "fuzz run recorded failures"
		}
		return "case failed"
	}
	f := res.Failures[0]
	s := f.Message
	if s == "" {
		s = f.Invariant
	}
	return s
}
