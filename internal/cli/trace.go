package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/jayimbery/bt/internal/config"
	"github.com/jayimbery/bt/internal/runplan"
	"github.com/jayimbery/bt/internal/trace/analyze"
	"github.com/jayimbery/bt/internal/trace/har"
	"github.com/jayimbery/bt/pkg/model"
)

func newTraceCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "trace",
		Short: "Import and inspect HAR-derived trace profiles",
	}
	c.AddCommand(newTraceImportCmd())
	c.AddCommand(newTraceInspectCmd())
	return c
}

func newTraceImportCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "import <har-file>",
		Short:         "Build a trace profile from a HAR file and the configured OpenAPI spec",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			adapterName := strings.TrimSpace(cfg.Target.Adapter)
			if adapterName == "" {
				adapterName = "openapi"
			}
			if !strings.EqualFold(adapterName, "openapi") {
				return fmt.Errorf("trace import: adapter %q is not supported (need openapi)", adapterName)
			}
			target := cfg.Target.AsModel()
			ad := runplan.AdapterForName(adapterName)
			if err := ad.Validate(cmd.Context(), target); err != nil {
				return fmt.Errorf("adapter validate: %w", err)
			}
			ops, err := ad.Discover(cmd.Context(), target)
			if err != nil {
				return fmt.Errorf("adapter discover: %w", err)
			}

			harPath := args[0]
			f, err := os.Open(harPath)
			if err != nil {
				return fmt.Errorf("open har: %w", err)
			}
			defer func() { _ = f.Close() }()
			h, err := har.Parse(f)
			if err != nil {
				return fmt.Errorf("parse har: %w", err)
			}

			profile, err := analyze.Analyze(h.Log.Entries, ops, filepath.Base(harPath))
			if err != nil {
				return fmt.Errorf("analyze: %w", err)
			}
			outPath := runplan.ResolveTraceProfilePath(cfgPath, cfg)
			if err := profile.WriteToFile(outPath); err != nil {
				return fmt.Errorf("write profile: %w", err)
			}

			var totalCalls int
			for _, op := range profile.Operations {
				if op != nil {
					totalCalls += op.CallCount
				}
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "trace import: wrote %d operations (%d matched calls) -> %s\n",
				len(profile.Operations), totalCalls, outPath)
			return nil
		},
	}
}

func newTraceInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:           "inspect",
		Short:         "Print a summary of the configured trace profile",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return err
			}
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return fmt.Errorf("config: %w", err)
			}
			path := runplan.ResolveTraceProfilePath(cfgPath, cfg)
			prof, err := model.ParseProfile(path)
			if err != nil {
				return fmt.Errorf("trace profile %q: %w", path, err)
			}
			writeTraceInspect(cmd.OutOrStdout(), prof, path)
			return nil
		},
	}
}

func writeTraceInspect(w io.Writer, prof *model.TraceProfile, path string) {
	_, _ = fmt.Fprintf(w, "Trace profile: %s\n", path)
	_, _ = fmt.Fprintf(w, "schema_version=%s generated_at=%s source_har=%q\n\n",
		prof.SchemaVersion, prof.GeneratedAt, prof.SourceHAR)

	ids := make([]string, 0, len(prof.Operations))
	for id := range prof.Operations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	_, _ = fmt.Fprintf(w, "Operations (%d):\n", len(ids))
	for _, id := range ids {
		op := prof.Operations[id]
		if op == nil {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %-40s calls=%d rank=%d\n", id, op.CallCount, op.FrequencyRank)
		argNames := make([]string, 0, len(op.Arguments))
		for a := range op.Arguments {
			argNames = append(argNames, a)
		}
		sort.Strings(argNames)
		for _, an := range argNames {
			ap := op.Arguments[an]
			if ap == nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "    arg %q type=%s samples=%d null_rate=%.3f always_present=%v\n",
				an, ap.Type, len(ap.Samples), ap.NullRate, ap.AlwaysPresent)
			if len(ap.Distribution) > 0 {
				_, _ = fmt.Fprintf(w, "      distribution: %#v\n", ap.Distribution)
			}
			if ap.Range != nil {
				_, _ = fmt.Fprintf(w, "      range: [%g, %g]\n", ap.Range.Min, ap.Range.Max)
			}
		}
	}
	if prof.Sequences != nil {
		_, _ = fmt.Fprintf(w, "\nSequences:\n")
		startKeys := make([]string, 0, len(prof.Sequences.StartProbability))
		for k := range prof.Sequences.StartProbability {
			startKeys = append(startKeys, k)
		}
		sort.Strings(startKeys)
		for _, k := range startKeys {
			_, _ = fmt.Fprintf(w, "  start %s -> %.4f\n", k, prof.Sequences.StartProbability[k])
		}
		for from, row := range prof.Sequences.Transitions {
			if len(row) == 0 {
				continue
			}
			type pair struct {
				to string
				p  float64
			}
			var pairs []pair
			for to, p := range row {
				pairs = append(pairs, pair{to: to, p: p})
			}
			sort.Slice(pairs, func(i, j int) bool { return pairs[i].p > pairs[j].p })
			n := 3
			if len(pairs) < n {
				n = len(pairs)
			}
			_, _ = fmt.Fprintf(w, "  transitions from %s (top %d):\n", from, n)
			for i := 0; i < n; i++ {
				_, _ = fmt.Fprintf(w, "    -> %s %.4f\n", pairs[i].to, pairs[i].p)
			}
		}
	}
}
