package runplan

import (
	"context"
	"strings"

	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/pkg/model"
)

// NewMergeHeaderExecutor wraps inner and fills unset headers from defaults (e.g. Authorization from target.auth).
func NewMergeHeaderExecutor(inner strategy.Executor, defaults map[string]string) strategy.Executor {
	if inner == nil {
		return inner
	}
	if len(defaults) == 0 {
		return inner
	}
	return &mergeHeaderExecutor{inner: inner, defaults: defaults}
}

type mergeHeaderExecutor struct {
	inner    strategy.Executor
	defaults map[string]string
}

func (m *mergeHeaderExecutor) Run(ctx context.Context, in model.CaseInput) (model.ResponseDetail, error) {
	merged := in
	if len(m.defaults) == 0 {
		return m.inner.Run(ctx, in)
	}
	if merged.Headers == nil {
		merged.Headers = make(map[string]string, len(m.defaults))
	} else {
		cp := make(map[string]string, len(merged.Headers)+len(m.defaults))
		for k, v := range merged.Headers {
			cp[k] = v
		}
		merged.Headers = cp
	}
	for k, v := range m.defaults {
		if strings.TrimSpace(merged.Headers[k]) == "" && strings.TrimSpace(v) != "" {
			merged.Headers[k] = v
		}
	}
	return m.inner.Run(ctx, merged)
}
