package runplan

import (
	"strings"
	"time"

	"github.com/jimbery/bt/internal/config"
	"github.com/jimbery/bt/internal/runner"
	gqlrunner "github.com/jimbery/bt/internal/runner/graphql"
	"github.com/jimbery/bt/internal/strategy"
)

// requestHTTPTimeout returns the HTTP client timeout for table/contract/property
// executors. When safety.timeout_seconds is unset or non-positive, it matches
// runner.DefaultTimeout (same default as fuzz per-request timeout).
func requestHTTPTimeout(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Safety.TimeoutSeconds <= 0 {
		return runner.DefaultTimeout
	}
	return time.Duration(cfg.Safety.TimeoutSeconds * float64(time.Second))
}

// BuildDefaultExecutor returns an HTTP executor, optionally wrapped for GraphQL POST
// bodies and target-derived headers (e.g. bearer Authorization).
func BuildDefaultExecutor(cfg *config.Config, adapterName string) strategy.Executor {
	to := requestHTTPTimeout(cfg)
	httpExec := runner.New(runner.Config{
		BaseURL: cfg.Target.BaseURL,
		Timeout: to,
	})
	var exec strategy.Executor = httpExec
	if strings.EqualFold(strings.TrimSpace(adapterName), "graphql") {
		gqlExec := gqlrunner.New(gqlrunner.Config{
			BaseURL: strings.TrimSpace(cfg.Target.BaseURL),
			Timeout: to,
		})
		exec = runner.NewGQLRESTExecutor(httpExec, gqlExec)
	}
	return NewMergeHeaderExecutor(exec, config.RequestHeaderOverrides(cfg.Target.Auth))
}
