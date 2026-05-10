package runplan

import (
	"strings"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/runner"
	gqlrunner "github.com/jayimbery/bt/internal/runner/graphql"
	"github.com/jayimbery/bt/internal/strategy"
)

// BuildDefaultExecutor returns an HTTP executor, optionally wrapped for GraphQL POST
// bodies and target-derived headers (e.g. bearer Authorization).
func BuildDefaultExecutor(cfg *config.Config, adapterName string) strategy.Executor {
	httpExec := runner.New(runner.Config{
		BaseURL: cfg.Target.BaseURL,
		Timeout: runner.DefaultTimeout,
	})
	var exec strategy.Executor = httpExec
	if strings.EqualFold(strings.TrimSpace(adapterName), "graphql") {
		gqlExec := gqlrunner.New(gqlrunner.Config{
			BaseURL: strings.TrimSpace(cfg.Target.BaseURL),
			Timeout: runner.DefaultTimeout,
		})
		exec = runner.NewGQLRESTExecutor(httpExec, gqlExec)
	}
	return NewMergeHeaderExecutor(exec, config.RequestHeaderOverrides(cfg.Target.Auth))
}
