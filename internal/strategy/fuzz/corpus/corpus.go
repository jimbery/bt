// Package corpus loads and persists fuzz seed inputs on disk.
package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/jimbery/bt/internal/strategy/fuzz/mutate"
)

// Corpus stores fuzz seeds under a directory.
type Corpus struct {
	dir    string
	loaded int
}

// NewCorpus returns a corpus backed by dir (created on first Save).
func NewCorpus(dir string) *Corpus {
	return &Corpus{dir: dir}
}

// Load reads all .json files as mutate.Input; malformed files are skipped with a warning.
func (c *Corpus) Load() ([]mutate.Input, error) {
	var out []mutate.Input
	_ = os.MkdirAll(c.dir, 0o755)
	entries, err := os.ReadDir(c.dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".json") {
			continue
		}
		path := filepath.Join(c.dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			log.Printf("corpus: skip read %s: %v", path, err)
			continue
		}
		var in mutate.Input
		if err := json.Unmarshal(data, &in); err != nil {
			log.Printf("corpus: skip invalid JSON %s: %v", path, err)
			continue
		}
		out = append(out, in)
	}
	c.loaded = len(out)
	return out, nil
}

// Save writes input as <sha256>.json; identical content is idempotent.
func (c *Corpus) Save(in mutate.Input) error {
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:]) + ".json"
	path := filepath.Join(c.dir, name)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}

// Size returns the entry count from the last successful Load.
func (c *Corpus) Size() int { return c.loaded }

// Dir returns the backing directory.
func (c *Corpus) Dir() string { return c.dir }
