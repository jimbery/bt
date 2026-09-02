package replay

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jimbery/bt/pkg/model"
)

// ErrArtifactNotFound is returned when the artifact file does not exist.
var ErrArtifactNotFound = errors.New("artifact not found")

// Loader reads artifact bundles from disk.
type Loader struct{}

// NewLoader returns a new Loader.
func NewLoader() *Loader { return &Loader{} }

// Load reads and deserialises an artifact from the given path.
func (l *Loader) Load(path string) (*model.Artifact, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrArtifactNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read artifact: %w", err)
	}

	var a model.Artifact
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("cannot parse artifact at %s: %w", path, err)
	}

	return &a, nil
}

// List returns the paths of all artifact JSON files in dir, sorted newest first.
func (l *Loader) List(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read artifact directory: %w", err)
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".json") {
			paths = append(paths, filepath.Join(dir, e.Name()))
		}
	}

	sort.Slice(paths, func(i, j int) bool {
		return paths[i] > paths[j]
	})

	return paths, nil
}
