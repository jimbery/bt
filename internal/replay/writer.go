package replay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jayimbery/bt/pkg/model"
)

const defaultArtifactDir = ".bt/artifacts"

// Writer writes artifact bundles to disk.
type Writer struct {
	dir string
}

// NewWriter returns a Writer that stores artifacts in dir.
// If dir is empty the default .bt/artifacts directory is used.
func NewWriter(dir string) *Writer {
	if dir == "" {
		dir = defaultArtifactDir
	}
	return &Writer{dir: dir}
}

// Write serialises the artifact to a JSON file and returns the path.
// The output directory is created if it does not exist.
// The filename is <timestamp>-<case-id>.json using a filesystem-safe timestamp.
func (w *Writer) Write(a model.Artifact) (string, error) {
	if err := os.MkdirAll(w.dir, 0o755); err != nil {
		return "", fmt.Errorf("cannot create artifact directory: %w", err)
	}

	ts := a.OccurredAt.UTC().Format("2006-01-02T150405Z")
	filename := fmt.Sprintf("%s-%s.json", ts, sanitise(a.CaseID))
	path := filepath.Join(w.dir, filename)

	data, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return "", fmt.Errorf("cannot marshal artifact: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", fmt.Errorf("cannot write artifact: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("cannot finalise artifact: %w", err)
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return path, fmt.Errorf("cannot resolve absolute artifact path: %w", err)
	}
	return abs, nil
}

func sanitise(s string) string {
	out := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' {
			out[i] = c
		} else {
			out[i] = '-'
		}
	}
	return string(out)
}

// DefaultArtifactDir returns the default artifact directory path.
func DefaultArtifactDir() string { return defaultArtifactDir }
