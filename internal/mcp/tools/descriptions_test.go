package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jayimbery/bt/internal/mcp/tools"
)

func TestToolDescriptions_AreNonEmpty(t *testing.T) {
	for _, tool := range tools.All() {
		if tool.Description == "" {
			t.Errorf("tool %q has empty description", tool.Name)
		}
	}
}

func TestToolDescriptions_AreAtLeast20Characters(t *testing.T) {
	for _, tool := range tools.All() {
		if len(tool.Description) < 20 {
			t.Errorf("tool %q description is too short (%d chars): %q",
				tool.Name, len(tool.Description), tool.Description)
		}
	}
}

func TestToolDescriptions_ContainToolName(t *testing.T) {
	for _, tool := range tools.All() {
		if !strings.Contains(tool.Description, tool.Name) {
			t.Errorf("tool %q description does not mention the tool's own name", tool.Name)
		}
	}
}

func TestToolNames_FollowBtPrefix(t *testing.T) {
	for _, tool := range tools.All() {
		if !strings.HasPrefix(tool.Name, "bt_") {
			t.Errorf("tool name %q must start with 'bt_'", tool.Name)
		}
	}
}

func TestToolInputSchemas_AreValidJSON(t *testing.T) {
	for _, tool := range tools.All() {
		var schema map[string]any
		if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
			t.Errorf("tool %q has invalid InputSchema JSON: %v", tool.Name, err)
		}
	}
}

func TestToolInputSchemas_HaveTypeObject(t *testing.T) {
	for _, tool := range tools.All() {
		var schema map[string]any
		_ = json.Unmarshal(tool.InputSchema, &schema)
		if schema["type"] != "object" {
			t.Errorf("tool %q InputSchema must have type=object, got %v", tool.Name, schema["type"])
		}
	}
}

func TestAllExpectedToolsAreRegistered(t *testing.T) {
	expected := []string{
		"bt_discover_operations",
		"bt_suggest_strategy",
		"bt_validate",
		"bt_scaffold_config",
		"bt_run",
		"bt_explain_failure",
	}
	registered := map[string]bool{}
	for _, tool := range tools.All() {
		registered[tool.Name] = true
	}
	for _, name := range expected {
		if !registered[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestToolDescriptions_AvoidImplementationLeaks(t *testing.T) {
	banned := []string{"github.com", "internal/", "func ", "struct{"}
	for _, tool := range tools.All() {
		lower := strings.ToLower(tool.Description)
		for _, b := range banned {
			if strings.Contains(lower, strings.ToLower(b)) {
				t.Errorf("tool %q description should not contain %q", tool.Name, b)
			}
		}
	}
}
