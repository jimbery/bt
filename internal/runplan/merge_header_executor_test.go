package runplan_test

import (
	"context"
	"testing"

	"github.com/jimbery/bt/internal/config"
	"github.com/jimbery/bt/internal/runplan"
	"github.com/jimbery/bt/pkg/model"
)

func TestNewMergeHeaderExecutor_fillsEmptyAuthorization(t *testing.T) {
	t.Parallel()
	inner := &captureExec{}
	ex := runplan.NewMergeHeaderExecutor(inner, map[string]string{"Authorization": "Bearer x"})
	_, err := ex.Run(context.Background(), model.CaseInput{
		Method: "GET",
		Path:   "/",
		Headers: map[string]string{
			"Authorization": "",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.last.Headers["Authorization"] != "Bearer x" {
		t.Errorf("Authorization: got %q", inner.last.Headers["Authorization"])
	}
}

func TestNewMergeHeaderExecutor_preservesExistingAuthorization(t *testing.T) {
	t.Parallel()
	inner := &captureExec{}
	ex := runplan.NewMergeHeaderExecutor(inner, map[string]string{"Authorization": "Bearer x"})
	_, err := ex.Run(context.Background(), model.CaseInput{
		Method: "GET",
		Path:   "/",
		Headers: map[string]string{
			"Authorization": "Bearer existing",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if inner.last.Headers["Authorization"] != "Bearer existing" {
		t.Errorf("Authorization: got %q", inner.last.Headers["Authorization"])
	}
}

type captureExec struct {
	last model.CaseInput
}

func (c *captureExec) Run(_ context.Context, in model.CaseInput) (model.ResponseDetail, error) {
	c.last = in
	return model.ResponseDetail{}, nil
}

func TestBuildDefaultExecutor_graphqlWrapsREST(t *testing.T) {
	t.Parallel()
	cfg := &config.Config{
		Target: config.TargetConfig{
			BaseURL: "https://example.com",
			Adapter: "graphql",
		},
	}
	ex := runplan.BuildDefaultExecutor(cfg, "graphql")
	if ex == nil {
		t.Fatal("expected non-nil executor")
	}
}
