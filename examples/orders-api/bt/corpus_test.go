package bt_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isAutoSavedCorpusFile matches fuzz corpus.Save output (<sha256>.json).
func isAutoSavedCorpusFile(name string) bool {
	base := strings.TrimSuffix(strings.ToLower(name), ".json")
	if len(base) != 64 {
		return false
	}
	for _, c := range base {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			continue
		}
		return false
	}
	return true
}

func TestCorpusFiles_AreValidJSON(t *testing.T) {
	entries, err := os.ReadDir("corpus")
	if err != nil {
		t.Fatalf("cannot read corpus directory: %v", err)
	}
	var jsonFiles int
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if isAutoSavedCorpusFile(entry.Name()) {
			continue
		}
		jsonFiles++
		data, err := os.ReadFile(filepath.Join("corpus", entry.Name()))
		if err != nil {
			t.Errorf("cannot read %s: %v", entry.Name(), err)
			continue
		}
		if !json.Valid(data) {
			t.Errorf("corpus file %s is not valid JSON", entry.Name())
		}
	}
	if jsonFiles == 0 {
		t.Fatal("corpus directory has no .json seed files")
	}
}

func TestCorpusFiles_HaveRequiredFields(t *testing.T) {
	entries, _ := os.ReadDir("corpus")
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if isAutoSavedCorpusFile(entry.Name()) {
			continue
		}
		data, err := os.ReadFile(filepath.Join("corpus", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var probe struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if probe.Method == "" {
			t.Errorf("corpus file %s: 'method' field is empty", entry.Name())
		}
		if probe.Path == "" {
			t.Errorf("corpus file %s: 'path' field is empty", entry.Name())
		}
	}
}

func TestCorpusFiles_MethodsAreUppercase(t *testing.T) {
	entries, _ := os.ReadDir("corpus")
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if isAutoSavedCorpusFile(entry.Name()) {
			continue
		}
		data, _ := os.ReadFile(filepath.Join("corpus", entry.Name()))
		var probe struct {
			Method string `json:"method"`
		}
		json.Unmarshal(data, &probe)
		if probe.Method != strings.ToUpper(probe.Method) {
			t.Errorf("corpus file %s: method %q should be uppercase", entry.Name(), probe.Method)
		}
	}
}

func TestCorpusFiles_NoDeleteMethods(t *testing.T) {
	entries, _ := os.ReadDir("corpus")
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if isAutoSavedCorpusFile(entry.Name()) {
			continue
		}
		data, _ := os.ReadFile(filepath.Join("corpus", entry.Name()))
		var probe struct {
			Method string `json:"method"`
		}
		json.Unmarshal(data, &probe)
		if strings.ToUpper(probe.Method) == "DELETE" {
			t.Errorf("corpus file %s: DELETE method found in safe corpus", entry.Name())
		}
	}
}

func TestCorpusFiles_PostBodiesAreValidJSON(t *testing.T) {
	entries, _ := os.ReadDir("corpus")
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if isAutoSavedCorpusFile(entry.Name()) {
			continue
		}
		data, _ := os.ReadFile(filepath.Join("corpus", entry.Name()))
		var probe struct {
			Method string          `json:"method"`
			Body   json.RawMessage `json:"body"`
		}
		if err := json.Unmarshal(data, &probe); err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		if strings.ToUpper(probe.Method) != "POST" || len(probe.Body) == 0 || string(probe.Body) == "null" {
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(probe.Body, &body); err != nil {
			t.Errorf("corpus file %s: POST body is not valid JSON: %v", entry.Name(), err)
		}
	}
}
