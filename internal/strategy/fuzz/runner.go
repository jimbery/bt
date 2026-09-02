package fuzz

import (
	"context"
	"log/slog"
	"time"

	"github.com/jimbery/bt/internal/replay"
	"github.com/jimbery/bt/internal/runner"
	"github.com/jimbery/bt/internal/strategy"
	"github.com/jimbery/bt/pkg/model"
)

// Runner is the M5 entry point that executes a [model.TestPlan] against a live base URL.
type Runner struct {
	artifactDir string
	log         *slog.Logger
}

// NewRunner returns a fuzz runner that writes replay artifacts under artifactDir.
func NewRunner(artifactDir string) *Runner {
	return &Runner{artifactDir: artifactDir, log: slog.Default()}
}

// NewRunnerWithLogger is like [NewRunner] but uses the given logger (nil uses [slog.Default]).
func NewRunnerWithLogger(log *slog.Logger, artifactDir string) *Runner {
	if log == nil {
		log = slog.Default()
	}
	return &Runner{artifactDir: artifactDir, log: log}
}

// Run executes plan using the fuzz strategy (safety, corpus, mutators, classifier).
func (r *Runner) Run(ctx context.Context, plan model.TestPlan) ([]model.Result, error) {
	cfg := plan.RunConfig
	opIDs := make([]string, 0, len(plan.Operations))
	for _, o := range plan.Operations {
		opIDs = append(opIDs, o.ID)
	}
	spec := strategy.Spec{
		Kind:       strategy.KindFuzz,
		Operations: opIDs,
		Config:     model.RunConfigToMap(cfg),
	}
	if cfg.FuzzIterations > 0 {
		spec.Config["fuzz_iterations"] = cfg.FuzzIterations
	}
	if cfg.CorpusDir != "" {
		spec.Config["corpus_dir"] = cfg.CorpusDir
	}
	spec.Config["fuzz_seed"] = time.Now().UnixNano()

	opts := Options{
		ArtifactWriter:       replay.NewWriter(r.artifactDir),
		Environment:          plan.Target.Environment,
		SafetyProfile:        cfg.Safety.Profile,
		AllowedMethods:       append([]string(nil), cfg.Safety.AllowedMethods...),
		DeniedMethods:        append([]string(nil), cfg.Safety.DeniedMethods...),
		MaxRequestsPerSecond: cfg.Safety.MaxRequestsPerSecond,
		MaxConcurrency:       cfg.Safety.MaxConcurrency,
		TimeoutSeconds:       cfg.Safety.TimeoutSeconds,
		DestructiveConfirmed: cfg.DestructiveConfirmed,
		Logger:               r.log,
	}
	st := NewWithOptions(opts)

	cases, err := st.Plan(ctx, spec, plan.Operations)
	if err != nil {
		return nil, err
	}
	exec := runner.New(runner.Config{
		BaseURL: plan.Target.BaseURL,
		Timeout: runner.DefaultTimeout,
	})
	return st.Execute(ctx, cases, exec)
}
