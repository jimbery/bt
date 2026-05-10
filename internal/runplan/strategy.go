// Package runplan builds strategy instances and specs for execution (CLI and MCP).
package runplan

import (
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/replay"
	"github.com/jayimbery/bt/internal/strategy"
	"github.com/jayimbery/bt/internal/strategy/contract"
	"github.com/jayimbery/bt/internal/strategy/fuzz"
	"github.com/jayimbery/bt/internal/strategy/property"
	"github.com/jayimbery/bt/internal/strategy/table"
	"github.com/jayimbery/bt/pkg/model"
)

// BuildOptions configures optional flags when constructing a strategy (mirrors CLI flags).
type BuildOptions struct {
	Stderr io.Writer

	SafetyFlagChanged bool
	SafetyFlagValue   string

	SeedProvided bool
	Seed         int64

	ChecksProvided bool
	Checks         int

	FuzzIterationsProvided bool
	FuzzIterations         int

	CorpusDirProvided bool
	CorpusDir         string

	// GQLExecutor is wired for table strategy when using the graphql adapter.
	GQLExecutor strategy.Executor
}

// BuildStrategyAndSpec returns a strategy implementation and its spec for the named strategy type.
func BuildStrategyAndSpec(cfgPath, strategyName string, cfg *config.Config, opt BuildOptions) (strategy.Strategy, strategy.Spec, error) {
	stderr := opt.Stderr
	if stderr == nil {
		stderr = io.Discard
	}

	var st strategy.Strategy
	destructiveConfirmed := opt.SafetyFlagChanged
	safetyFlag := ""
	if destructiveConfirmed {
		safetyFlag = opt.SafetyFlagValue
		destructiveConfirmed = strings.EqualFold(strings.TrimSpace(safetyFlag), "destructive")
	}
	profile := strings.TrimSpace(cfg.Safety.Profile)
	if opt.SafetyFlagChanged {
		profile = strings.TrimSpace(safetyFlag)
	}
	if strategy.Kind(strategyName) == strategy.KindFuzz {
		if strings.EqualFold(profile, "destructive") && !destructiveConfirmed {
			return nil, strategy.Spec{}, fmt.Errorf(`fuzz: profile "destructive" requires passing --safety destructive on the command line`)
		}
		if err := validateFuzzSafetyProfileName(profile); err != nil {
			return nil, strategy.Spec{}, err
		}
	}

	switch strategy.Kind(strategyName) {
	case strategy.KindTable:
		artifactDir := filepath.Join(filepath.Dir(cfgPath), ".bt", "artifacts")
		st = table.NewWithOptions(table.Options{
			ArtifactWriter: replay.NewWriter(artifactDir),
			Environment:    cfg.Target.Environment,
			GQLExecutor:      opt.GQLExecutor,
		})
	case strategy.KindProperty:
		artifactDir := filepath.Join(filepath.Dir(cfgPath), ".bt", "artifacts")
		st = property.NewWithOptions(property.Options{
			ArtifactWriter: replay.NewWriter(artifactDir),
			Environment:    cfg.Target.Environment,
		})
	case strategy.KindFuzz:
		artifactDir := filepath.Join(filepath.Dir(cfgPath), ".bt", "artifacts")
		fuzzLog := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		st = fuzz.NewWithOptions(fuzz.Options{
			ArtifactWriter:       replay.NewWriter(artifactDir),
			Environment:          cfg.Target.Environment,
			SafetyProfile:        profile,
			AllowedMethods:       append([]string(nil), cfg.Safety.AllowedMethods...),
			DeniedMethods:        append([]string(nil), cfg.Safety.DenyMethods...),
			MaxRequestsPerSecond: cfg.Safety.MaxRequestsPerSecond,
			MaxConcurrency:       cfg.Safety.MaxConcurrency,
			TimeoutSeconds:       cfg.Safety.TimeoutSeconds,
			DestructiveConfirmed: destructiveConfirmed,
			Logger:               fuzzLog,
		})
	case strategy.KindContract:
		artifactDir := filepath.Join(filepath.Dir(cfgPath), ".bt", "artifacts")
		st = contract.NewWithOptions(contract.Options{
			ArtifactWriter: replay.NewWriter(artifactDir),
			Environment:    cfg.Target.Environment,
		})
	default:
		return nil, strategy.Spec{}, fmt.Errorf("unknown strategy: %q", strategyName)
	}

	spec := strategy.Spec{Kind: strategy.Kind(strategyName)}
	var found bool
	for _, sc := range cfg.Strategies {
		if sc.Type != strategyName {
			continue
		}
		found = true
		spec.Operations = append([]string(nil), sc.Operations...)
		for _, name := range sc.Invariants {
			spec.Invariants = append(spec.Invariants, model.Invariant{Name: name})
		}
		if sc.Config != nil {
			spec.Config = make(map[string]any, len(sc.Config)+1)
			for k, v := range sc.Config {
				spec.Config[k] = v
			}
		} else {
			spec.Config = map[string]any{}
		}
		if sc.File != "" {
			spec.Config["file"] = sc.File
		}
		if strategy.Kind(strategyName) == strategy.KindFuzz {
			if _, ok := spec.Config["corpus_dir"]; !ok || spec.Config["corpus_dir"] == "" {
				spec.Config["corpus_dir"] = filepath.Join(filepath.Dir(cfgPath), "corpus")
			}
		}
		break
	}
	if !found {
		return nil, strategy.Spec{}, fmt.Errorf("no strategy of type %q found in config", strategyName)
	}

	if strategy.Kind(strategyName) == strategy.KindProperty {
		if opt.SeedProvided {
			if spec.Config == nil {
				spec.Config = map[string]any{}
			}
			spec.Config["seed"] = opt.Seed
		}
		if opt.ChecksProvided {
			if spec.Config == nil {
				spec.Config = map[string]any{}
			}
			spec.Config["checks"] = opt.Checks
		}
	}
	if strategy.Kind(strategyName) == strategy.KindFuzz {
		if spec.Config == nil {
			spec.Config = map[string]any{}
		}
		if opt.FuzzIterationsProvided {
			spec.Config["fuzz_iterations"] = opt.FuzzIterations
		}
		if opt.CorpusDirProvided {
			spec.Config["corpus_dir"] = opt.CorpusDir
		}
	}

	return st, spec, nil
}

func validateFuzzSafetyProfileName(p string) error {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "safe", "aggressive", "destructive":
		return nil
	default:
		return fmt.Errorf("fuzz: unknown safety profile %q (want safe, aggressive, or destructive)", p)
	}
}
