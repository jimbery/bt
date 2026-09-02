package corpus_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jimbery/bt/internal/strategy/fuzz/corpus"
	"github.com/jimbery/bt/internal/strategy/fuzz/mutate"
)

func sampleInput(body string) mutate.Input {
	return mutate.Input{
		Method:  "POST",
		Path:    "/orders",
		Query:   map[string]string{},
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(body),
	}
}

// --- Load ---

func TestCorpus_Load_EmptyDir_ReturnsEmptySlice(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	entries, err := c.Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error on empty dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from empty dir, got %d", len(entries))
	}
}

func TestCorpus_Load_ReadsValidJSONFiles(t *testing.T) {
	dir := t.TempDir()
	input := sampleInput(`{"amount":10,"currency":"GBP"}`)
	data, _ := json.Marshal(input)
	if err := os.WriteFile(filepath.Join(dir, "entry.json"), data, 0644); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}

	c := corpus.NewCorpus(dir)
	entries, err := c.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestCorpus_Load_SkipsMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	// Write a valid entry and a malformed entry.
	input := sampleInput(`{"amount":1}`)
	data, _ := json.Marshal(input)
	os.WriteFile(filepath.Join(dir, "valid.json"), data, 0644)
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte("not json"), 0644)

	c := corpus.NewCorpus(dir)
	entries, err := c.Load()
	if err != nil {
		t.Fatalf("Load should not error on malformed files, got: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (malformed file skipped), got %d", len(entries))
	}
}

func TestCorpus_Load_IgnoresNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a corpus entry"), 0644)

	c := corpus.NewCorpus(dir)
	entries, err := c.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries (only .json files loaded), got %d", len(entries))
	}
}

// --- Save ---

func TestCorpus_Save_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "corpus")
	c := corpus.NewCorpus(dir)
	if err := c.Save(sampleInput(`{"amount":1}`)); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Save must create the corpus directory if it does not exist")
	}
}

func TestCorpus_Save_WritesJSONFile(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	if err := c.Save(sampleInput(`{"amount":2}`)); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	entries, _ := c.Load()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after Save, got %d", len(entries))
	}
}

func TestCorpus_Save_SameContent_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	input := sampleInput(`{"amount":3}`)
	c.Save(input)
	c.Save(input) // identical content — should not create a second file
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Errorf("expected 1 file after saving same content twice, got %d", len(files))
	}
}

func TestCorpus_Save_DifferentContent_CreatesSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	c.Save(sampleInput(`{"amount":1}`))
	c.Save(sampleInput(`{"amount":2}`))
	files, _ := os.ReadDir(dir)
	if len(files) != 2 {
		t.Errorf("expected 2 files for different content, got %d", len(files))
	}
}

func TestCorpus_Save_ProducesValidJSON(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	c.Save(sampleInput(`{"amount":5}`))
	files, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	var decoded mutate.Input
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("saved corpus file is not valid JSON: %v", err)
	}
}

func TestCorpus_Save_RoundTrip_PreservesBody(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	original := sampleInput(`{"amount":77,"currency":"EUR"}`)
	c.Save(original)
	entries, _ := c.Load()
	if len(entries) == 0 {
		t.Fatal("no entries after Save+Load")
	}
	var got, want any
	if err := json.Unmarshal(entries[0].Body, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(original.Body, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("body mismatch: got %v, want %v", got, want)
	}
}

// --- Size ---

func TestCorpus_Size_EmptyDir_IsZero(t *testing.T) {
	c := corpus.NewCorpus(t.TempDir())
	c.Load()
	if c.Size() != 0 {
		t.Errorf("expected size 0 on empty corpus, got %d", c.Size())
	}
}

func TestCorpus_Size_AfterLoad_ReflectsEntryCount(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	c.Save(sampleInput(`{"amount":1}`))
	c.Save(sampleInput(`{"amount":2}`))
	c.Load()
	if c.Size() != 2 {
		t.Errorf("expected size 2 after loading 2 entries, got %d", c.Size())
	}
}
