// Package testutil holds small helpers shared by tests across the module.
package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// RepoRoot walks upward from the caller's source file until a directory containing go.mod is found.
func RepoRoot(t testing.TB) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for range 32 {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	t.Fatalf("go.mod not found from %s", file)
	panic("unreachable")
}

// SortedStringKeys returns map keys sorted for stable assertion messages.
func SortedStringKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
