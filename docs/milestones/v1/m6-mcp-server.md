# M6 — MCP Server

This document follows the same structure as M1–M5: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist. Tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

---

## Overview

M6 exposes `bt` as an MCP (Model Context Protocol) server. Any MCP-compatible client — Claude, Cursor, Zed, or anything implementing the protocol — can orchestrate the platform by calling structured tools. The execution engine is unchanged; the MCP server is a second entry point into it alongside the CLI.

The four pieces built here are:

1. **Tool registry and dispatch** — the internal registry that maps tool names to handler functions, validates inputs against JSON Schema, and routes calls to the engine
2. **Six tools** — `bt_discover_operations`, `bt_suggest_strategy`, `bt_validate`, `bt_scaffold_config`, `bt_run`, `bt_explain_failure` — each with a precise JSON Schema for inputs and outputs
3. **`bt mcp serve` subcommand** — the long-running MCP protocol server wired to stdio transport
4. **Structured JSON output contract** — every tool response has a defined schema; the artifact filesystem is shared state between `bt_run` and `bt_explain_failure`

Each piece has its own spec, tests, and implementation section. Build and verify in order.

**Exit criterion:** Any MCP-compatible client can call `bt_run` against a real API and receive a structured summary with an artifact path. `bt_explain_failure` takes that path and returns structured failure detail. All tools respond correctly to malformed inputs.

---

## Step 1 — Tool registry

### Spec

The tool registry lives at `internal/mcp/registry`. It is the dispatch layer between the MCP protocol and the engine.

- `Tool` describes a single MCP tool:
  ```go
  type Tool struct {
      Name        string          // e.g. "bt_run"
      Description string          // shown to the AI client; must be unambiguous
      InputSchema json.RawMessage // JSON Schema object for the input
      Handler     HandlerFunc
  }
  ```
- `HandlerFunc` is `func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)`
- `Registry` holds the registered tools:
  - `Register(tool Tool) error` — adds a tool; returns an error if the name is already registered or empty
  - `Dispatch(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error)` — looks up the tool by name and calls its handler; returns a typed error if the tool is not found
  - `List() []ToolSummary` — returns name + description for all registered tools, sorted alphabetically by name
  - `Get(name string) (Tool, bool)` — returns the tool by name
- Input JSON is validated against the tool's `InputSchema` before the handler is called; malformed or schema-invalid input returns a structured error without calling the handler
- `Dispatch` wraps handler panics in a recoverable error — a panicking handler must not crash the server
- The registry is safe for concurrent reads; `Register` is not called after server start

### Tests

`internal/mcp/registry/registry_test.go`:

```go
package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jimbery/bt/internal/mcp/registry"
)

// --- Helpers ---

func echoHandler(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return input, nil
}

func errorHandler(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	return nil, errors.New("handler error")
}

func panicHandler(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	panic("handler panicked")
}

func simpleTool(name string) registry.Tool {
	return registry.Tool{
		Name:        name,
		Description: "A test tool",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`),
		Handler:     echoHandler,
	}
}

// --- Register ---

func TestRegistry_Register_AddsToolSuccessfully(t *testing.T) {
	r := registry.New()
	if err := r.Register(simpleTool("bt_test")); err != nil {
		t.Fatalf("Register returned unexpected error: %v", err)
	}
}

func TestRegistry_Register_EmptyName_ReturnsError(t *testing.T) {
	r := registry.New()
	tool := simpleTool("")
	if err := r.Register(tool); err == nil {
		t.Error("expected error when registering tool with empty name")
	}
}

func TestRegistry_Register_DuplicateName_ReturnsError(t *testing.T) {
	r := registry.New()
	r.Register(simpleTool("bt_test"))
	if err := r.Register(simpleTool("bt_test")); err == nil {
		t.Error("expected error when registering duplicate tool name")
	}
}

func TestRegistry_Register_EmptyDescription_ReturnsError(t *testing.T) {
	r := registry.New()
	tool := registry.Tool{
		Name:        "bt_test",
		Description: "",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:     echoHandler,
	}
	if err := r.Register(tool); err == nil {
		t.Error("expected error when registering tool with empty description")
	}
}

func TestRegistry_Register_NilHandler_ReturnsError(t *testing.T) {
	r := registry.New()
	tool := registry.Tool{
		Name:        "bt_test",
		Description: "test",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:     nil,
	}
	if err := r.Register(tool); err == nil {
		t.Error("expected error when registering tool with nil handler")
	}
}

// --- Get ---

func TestRegistry_Get_KnownTool_ReturnsTool(t *testing.T) {
	r := registry.New()
	r.Register(simpleTool("bt_run"))
	tool, ok := r.Get("bt_run")
	if !ok {
		t.Fatal("expected Get to find registered tool")
	}
	if tool.Name != "bt_run" {
		t.Errorf("expected Name=%q, got %q", "bt_run", tool.Name)
	}
}

func TestRegistry_Get_UnknownTool_ReturnsFalse(t *testing.T) {
	r := registry.New()
	_, ok := r.Get("bt_unknown")
	if ok {
		t.Error("expected Get to return false for unknown tool")
	}
}

// --- List ---

func TestRegistry_List_ReturnsAllRegisteredTools(t *testing.T) {
	r := registry.New()
	r.Register(simpleTool("bt_run"))
	r.Register(simpleTool("bt_validate"))
	r.Register(simpleTool("bt_discover_operations"))

	summaries := r.List()
	if len(summaries) != 3 {
		t.Errorf("expected 3 tool summaries, got %d", len(summaries))
	}
}

func TestRegistry_List_IsSortedAlphabetically(t *testing.T) {
	r := registry.New()
	r.Register(simpleTool("bt_validate"))
	r.Register(simpleTool("bt_run"))
	r.Register(simpleTool("bt_discover_operations"))

	summaries := r.List()
	names := make([]string, len(summaries))
	for i, s := range summaries {
		names[i] = s.Name
	}

	expected := []string{"bt_discover_operations", "bt_run", "bt_validate"}
	for i, name := range expected {
		if names[i] != name {
			t.Errorf("list[%d]: expected %q, got %q", i, name, names[i])
		}
	}
}

func TestRegistry_List_Empty_ReturnsEmptySlice(t *testing.T) {
	r := registry.New()
	if summaries := r.List(); len(summaries) != 0 {
		t.Errorf("expected empty list, got %d entries", len(summaries))
	}
}

// --- Dispatch ---

func TestRegistry_Dispatch_KnownTool_CallsHandler(t *testing.T) {
	r := registry.New()
	called := false
	tool := registry.Tool{
		Name:        "bt_test",
		Description: "test",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			called = true
			return json.RawMessage(`{}`), nil
		},
	}
	r.Register(tool)

	_, err := r.Dispatch(context.Background(), "bt_test", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Dispatch returned unexpected error: %v", err)
	}
	if !called {
		t.Error("expected handler to be called")
	}
}

func TestRegistry_Dispatch_UnknownTool_ReturnsError(t *testing.T) {
	r := registry.New()
	_, err := r.Dispatch(context.Background(), "bt_unknown", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestRegistry_Dispatch_HandlerError_PropagatesError(t *testing.T) {
	r := registry.New()
	tool := registry.Tool{
		Name:        "bt_error",
		Description: "test",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:     errorHandler,
	}
	r.Register(tool)

	_, err := r.Dispatch(context.Background(), "bt_error", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected error from handler to propagate")
	}
}

func TestRegistry_Dispatch_PanickingHandler_ReturnsError(t *testing.T) {
	r := registry.New()
	tool := registry.Tool{
		Name:        "bt_panic",
		Description: "test",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler:     panicHandler,
	}
	r.Register(tool)

	_, err := r.Dispatch(context.Background(), "bt_panic", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected recovered panic to be returned as error")
	}
}

func TestRegistry_Dispatch_InvalidInputJSON_ReturnsErrorBeforeCallingHandler(t *testing.T) {
	r := registry.New()
	called := false
	tool := registry.Tool{
		Name:        "bt_test",
		Description: "test",
		InputSchema: json.RawMessage(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`),
		Handler: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			called = true
			return json.RawMessage(`{}`), nil
		},
	}
	r.Register(tool)

	// Missing required "name" field.
	_, err := r.Dispatch(context.Background(), "bt_test", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected schema validation error for missing required field")
	}
	if called {
		t.Error("handler must not be called when input fails schema validation")
	}
}

func TestRegistry_Dispatch_MalformedJSON_ReturnsError(t *testing.T) {
	r := registry.New()
	r.Register(simpleTool("bt_test"))
	_, err := r.Dispatch(context.Background(), "bt_test", json.RawMessage(`not valid json`))
	if err == nil {
		t.Error("expected error for malformed JSON input")
	}
}

func TestRegistry_Dispatch_ContextCancellation_PropagatesCancel(t *testing.T) {
	r := registry.New()
	tool := registry.Tool{
		Name:        "bt_slow",
		Description: "test",
		InputSchema: json.RawMessage(`{"type":"object"}`),
		Handler: func(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	r.Register(tool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Dispatch(ctx, "bt_slow", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected context cancellation to propagate from handler")
	}
}
```

---

## Step 2 — Tool definitions and handlers

### Spec

Six tools are implemented in M6. Each lives in `internal/mcp/tools/`. Each tool has a defined input schema, a defined output schema, and a handler that calls into the engine. Tool descriptions must be precise enough that any MCP client can select the correct tool from the description alone.

---

### `bt_discover_operations`

**Description:** `"Parse an OpenAPI schema file and return all discovered operations as a structured list. Use this before bt_suggest_strategy or bt_scaffold_config."`

**Input schema:**
```json
{
  "type": "object",
  "required": ["schema_path"],
  "properties": {
    "schema_path": {
      "type": "string",
      "description": "Absolute or relative path to an OpenAPI 3.x schema file (YAML or JSON)"
    }
  }
}
```

**Output schema:**
```json
{
  "type": "object",
  "required": ["operations", "operation_count"],
  "properties": {
    "operations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "method", "path"],
        "properties": {
          "id":          { "type": "string" },
          "method":      { "type": "string" },
          "path":        { "type": "string" },
          "summary":     { "type": "string" },
          "has_body":    { "type": "boolean" },
          "param_count": { "type": "integer" },
          "response_codes": {
            "type": "array",
            "items": { "type": "integer" }
          }
        }
      }
    },
    "operation_count": { "type": "integer" }
  }
}
```

**Behaviour:**
- Calls `adapter.Discover` with the given schema path
- Returns a condensed summary of each operation (not the full schema tree — that would be too large for MCP context)
- If the schema file does not exist or cannot be parsed, returns a structured error response (not a Go error): `{"error": "...", "code": "SCHEMA_PARSE_ERROR"}`

---

### `bt_suggest_strategy`

**Description:** `"Given a list of operation IDs from bt_discover_operations, recommend which bt testing strategies are most appropriate for each operation and why."`

**Input schema:**
```json
{
  "type": "object",
  "required": ["operations"],
  "properties": {
    "operations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["id", "method"],
        "properties": {
          "id":       { "type": "string" },
          "method":   { "type": "string" },
          "has_body": { "type": "boolean" }
        }
      }
    }
  }
}
```

**Output schema:**
```json
{
  "type": "object",
  "required": ["recommendations"],
  "properties": {
    "recommendations": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["operation_id", "strategies"],
        "properties": {
          "operation_id": { "type": "string" },
          "strategies": {
            "type": "array",
            "items": {
              "type": "object",
              "required": ["strategy", "rationale", "priority"],
              "properties": {
                "strategy":  { "type": "string", "enum": ["table", "property", "fuzz", "contract"] },
                "rationale": { "type": "string" },
                "priority":  { "type": "string", "enum": ["recommended", "optional", "not_applicable"] }
              }
            }
          }
        }
      }
    }
  }
}
```

**Behaviour — pure logic, no AI in M6:**
- `GET` with no body → `table: recommended`, `property: optional`, `fuzz: optional`
- `POST` or `PUT` with body → `table: recommended`, `property: recommended`, `fuzz: recommended`
- `PATCH` → `table: recommended`, `property: optional`, `fuzz: optional`
- `DELETE` → `table: recommended`, `fuzz: optional`, `property: not_applicable`
- All operations get `contract: optional` — contract testing is always valid but not the first priority
- This logic is deterministic and does not call any AI service — M7 replaces it with AI-based suggestions

---

### `bt_validate`

**Description:** `"Validate a bt config file. Returns a list of validation errors if the config is invalid, or an empty errors list if valid. Run this before bt_run."`

**Input schema:**
```json
{
  "type": "object",
  "required": ["config_path"],
  "properties": {
    "config_path": {
      "type": "string",
      "description": "Path to a backendtest.yaml config file"
    }
  }
}
```

**Output schema:**
```json
{
  "type": "object",
  "required": ["valid", "errors"],
  "properties": {
    "valid":  { "type": "boolean" },
    "errors": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["field", "message"],
        "properties": {
          "field":   { "type": "string" },
          "message": { "type": "string" }
        }
      }
    }
  }
}
```

**Behaviour:**
- Calls the existing config loader and validator
- Returns `{"valid": true, "errors": []}` on success
- Returns `{"valid": false, "errors": [...]}` on any validation failure — every distinct error is a separate entry with its field path

---

### `bt_scaffold_config`

**Description:** `"Generate a starter backendtest.yaml config file from an OpenAPI schema and optional strategy recommendations. Returns the config as a string — write it to a file to use it."`

**Input schema:**
```json
{
  "type": "object",
  "required": ["schema_path"],
  "properties": {
    "schema_path": {
      "type": "string",
      "description": "Path to an OpenAPI 3.x schema file"
    },
    "base_url": {
      "type": "string",
      "description": "Target base URL, e.g. http://localhost:8080. Defaults to http://localhost:8080 if omitted."
    },
    "strategies": {
      "type": "array",
      "items": { "type": "string", "enum": ["table", "property", "fuzz", "contract"] },
      "description": "Strategies to include. Defaults to [table] if omitted."
    },
    "output_path": {
      "type": "string",
      "description": "If provided, write the generated config to this path on disk. If omitted, return it as a string only."
    }
  }
}
```

**Output schema:**
```json
{
  "type": "object",
  "required": ["config_yaml", "written_to_disk"],
  "properties": {
    "config_yaml":     { "type": "string" },
    "written_to_disk": { "type": "boolean" },
    "output_path":     { "type": "string" }
  }
}
```

**Behaviour:**
- Calls `adapter.Discover` to get operations from the schema
- Generates a valid `backendtest.yaml` with sensible defaults: safe profile, table strategy with placeholder case file, report formats `[console, json]`
- If `strategies` includes `property`, adds a property strategy block with default invariants
- If `output_path` is provided, writes the file to disk and sets `written_to_disk: true`
- Generated YAML must be parseable by the existing config loader without modification

---

### `bt_run`

**Description:** `"Execute a bt test plan using the specified config and strategy. Returns a structured summary and an artifact directory path. For detailed failure information, call bt_explain_failure with a specific artifact path."`

**Input schema:**
```json
{
  "type": "object",
  "required": ["config_path"],
  "properties": {
    "config_path": {
      "type": "string",
      "description": "Path to a backendtest.yaml config file"
    },
    "strategy": {
      "type": "string",
      "enum": ["table", "property", "fuzz"],
      "description": "Strategy to run. Defaults to the first strategy in the config if omitted."
    },
    "seed": {
      "type": "integer",
      "description": "Seed for deterministic property or fuzz runs. 0 means random."
    }
  }
}
```

**Output schema:**
```json
{
  "type": "object",
  "required": ["passed", "failed", "skipped", "total", "strategy", "duration_ms", "artifact_dir", "failures"],
  "properties": {
    "passed":       { "type": "integer" },
    "failed":       { "type": "integer" },
    "skipped":      { "type": "integer" },
    "total":        { "type": "integer" },
    "strategy":     { "type": "string" },
    "duration_ms":  { "type": "integer" },
    "artifact_dir": { "type": "string", "description": "Directory containing artifact files for failed cases" },
    "failures": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["case_id", "artifact_path", "summary"],
        "properties": {
          "case_id":       { "type": "string" },
          "artifact_path": { "type": "string" },
          "summary":       { "type": "string", "description": "One-line human-readable summary of the failure" }
        }
      }
    }
  }
}
```

**Behaviour:**
- Loads and validates the config; if invalid, returns a structured error (not a Go error)
- Runs the engine with the specified strategy
- Returns a compact summary — **not** full request/response payloads — to avoid oversized MCP responses
- Every failed case is listed with its artifact path so `bt_explain_failure` can be called for detail
- `duration_ms` is the wall-clock time for the entire run

---

### `bt_explain_failure`

**Description:** `"Load a bt failure artifact and return structured detail about what failed, what request was sent, and what response was received. Use this after bt_run reports a failure."`

**Input schema:**
```json
{
  "type": "object",
  "required": ["artifact_path"],
  "properties": {
    "artifact_path": {
      "type": "string",
      "description": "Path to a .json artifact file produced by bt_run or bt run"
    }
  }
}
```

**Output schema:**
```json
{
  "type": "object",
  "required": ["case_id", "strategy", "occurred_at", "request", "response", "failures"],
  "properties": {
    "case_id":     { "type": "string" },
    "strategy":    { "type": "string" },
    "occurred_at": { "type": "string", "format": "date-time" },
    "seed":        { "type": "integer" },
    "request": {
      "type": "object",
      "required": ["method", "url"],
      "properties": {
        "method":  { "type": "string" },
        "url":     { "type": "string" },
        "headers": { "type": "object" },
        "body":    { "type": "string" }
      }
    },
    "response": {
      "type": "object",
      "required": ["status_code"],
      "properties": {
        "status_code": { "type": "integer" },
        "headers":     { "type": "object" },
        "body":        { "type": "string" }
      }
    },
    "failures": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["invariant", "message"],
        "properties": {
          "invariant":      { "type": "string" },
          "message":        { "type": "string" },
          "path":           { "type": "string" },
          "expected":       { "type": "string" },
          "actual":         { "type": "string" },
          "classification": { "type": "string" }
        }
      }
    },
    "replay_command": { "type": "string", "description": "The bt replay command to reproduce this failure" }
  }
}
```

**Behaviour:**
- Reads the artifact JSON from disk
- Returns the full detail — request, response, all failures — since this is an on-demand call for a specific failure, not a bulk summary
- `replay_command` is constructed from the artifact path: `bt replay --config <inferred> <artifact_path>`
- If the artifact file does not exist, returns a structured error: `{"error": "artifact not found", "code": "ARTIFACT_NOT_FOUND", "path": "<path>"}`

---

### Tests

`internal/mcp/tools/tools_test.go`:

```go
package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jimbery/bt/internal/mcp/tools"
	"github.com/jimbery/bt/pkg/model"
)

// --- Helpers ---

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("cannot marshal: %v", err)
	}
	return b
}

func mustUnmarshal(t *testing.T, data json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("cannot unmarshal response: %v\nraw: %s", err, data)
	}
}

func sampleArtifact() model.Artifact {
	return model.Artifact{
		ID:           "test-artifact",
		CaseID:       "CreateOrder",
		StrategyKind: "table",
		Request: model.RequestDetail{
			Method:  "POST",
			URL:     "http://localhost:8080/orders",
			Headers: map[string]string{"Content-Type": "application/json"},
			Body:    []byte(`{"amount":99.99,"currency":"GBP"}`),
		},
		Response: model.ResponseDetail{
			StatusCode: 500,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       []byte(`{"error":"internal server error","code":"INTERNAL"}`),
		},
		Failures: []model.Failure{
			{Invariant: "no_5xx", Message: "expected < 500, got 500", Expected: "< 500", Actual: "500"},
		},
	}
}

// --- bt_discover_operations ---

func TestDiscoverOperations_MissingSchemaPath_ReturnsValidationError(t *testing.T) {
	h := tools.DiscoverOperationsHandler()
	input := mustMarshal(t, map[string]any{}) // schema_path is required but missing
	_, err := h(context.Background(), input)
	if err == nil {
		t.Error("expected error for missing required schema_path")
	}
}

func TestDiscoverOperations_NonexistentFile_ReturnsStructuredError(t *testing.T) {
	h := tools.DiscoverOperationsHandler()
	input := mustMarshal(t, map[string]any{
		"schema_path": "/does/not/exist/openapi.yaml",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured error response, not Go error: %v", err)
	}
	var resp map[string]any
	mustUnmarshal(t, result, &resp)
	if _, hasErr := resp["error"]; !hasErr {
		t.Error("expected 'error' field in response for missing file")
	}
	if resp["code"] != "SCHEMA_PARSE_ERROR" {
		t.Errorf("expected code SCHEMA_PARSE_ERROR, got %v", resp["code"])
	}
}

func TestDiscoverOperations_ValidSchema_ReturnsOperations(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.DiscoverOperationsHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Operations     []map[string]any `json:"operations"`
		OperationCount int              `json:"operation_count"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.OperationCount == 0 {
		t.Error("expected at least one operation from a valid schema")
	}
	if len(resp.Operations) != resp.OperationCount {
		t.Errorf("OperationCount=%d but Operations has %d entries", resp.OperationCount, len(resp.Operations))
	}
}

func TestDiscoverOperations_EachOperation_HasRequiredFields(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.DiscoverOperationsHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		Operations []map[string]any `json:"operations"`
	}
	mustUnmarshal(t, result, &resp)
	for _, op := range resp.Operations {
		for _, field := range []string{"id", "method", "path"} {
			if _, ok := op[field]; !ok {
				t.Errorf("operation missing required field %q: %v", field, op)
			}
		}
	}
}

// --- bt_suggest_strategy ---

func TestSuggestStrategy_EmptyOperations_ReturnsEmptyRecommendations(t *testing.T) {
	h := tools.SuggestStrategyHandler()
	input := mustMarshal(t, map[string]any{"operations": []any{}})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Recommendations []any `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	if len(resp.Recommendations) != 0 {
		t.Errorf("expected empty recommendations for empty operations, got %d", len(resp.Recommendations))
	}
}

func TestSuggestStrategy_POSTWithBody_RecommendsPropertyAndFuzz(t *testing.T) {
	h := tools.SuggestStrategyHandler()
	input := mustMarshal(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Recommendations []struct {
			OperationID string `json:"operation_id"`
			Strategies  []struct {
				Strategy string `json:"strategy"`
				Priority string `json:"priority"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	if len(resp.Recommendations) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(resp.Recommendations))
	}
	rec := resp.Recommendations[0]
	if rec.OperationID != "CreateOrder" {
		t.Errorf("expected OperationID=CreateOrder, got %q", rec.OperationID)
	}
	recommended := map[string]bool{}
	for _, s := range rec.Strategies {
		if s.Priority == "recommended" {
			recommended[s.Strategy] = true
		}
	}
	for _, expected := range []string{"table", "property", "fuzz"} {
		if !recommended[expected] {
			t.Errorf("expected %q to be 'recommended' for POST with body", expected)
		}
	}
}

func TestSuggestStrategy_GETNoBody_DoesNotRecommendPropertyAsPrimary(t *testing.T) {
	h := tools.SuggestStrategyHandler()
	input := mustMarshal(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "GetOrder", "method": "GET", "has_body": false},
		},
	})
	result, _ := h(context.Background(), input)
	var resp struct {
		Recommendations []struct {
			Strategies []struct {
				Strategy string `json:"strategy"`
				Priority string `json:"priority"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	for _, s := range resp.Recommendations[0].Strategies {
		if s.Strategy == "property" && s.Priority == "recommended" {
			t.Error("property should not be 'recommended' (only 'optional') for GET with no body")
		}
	}
}

func TestSuggestStrategy_AllOperations_HaveTableRecommended(t *testing.T) {
	// Table testing is always recommended for every operation type.
	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	h := tools.SuggestStrategyHandler()
	for _, method := range methods {
		input := mustMarshal(t, map[string]any{
			"operations": []any{
				map[string]any{"id": "Op", "method": method, "has_body": method == "POST"},
			},
		})
		result, _ := h(context.Background(), input)
		var resp struct {
			Recommendations []struct {
				Strategies []struct {
					Strategy string `json:"strategy"`
					Priority string `json:"priority"`
				} `json:"strategies"`
			} `json:"recommendations"`
		}
		mustUnmarshal(t, result, &resp)
		tableRecommended := false
		for _, s := range resp.Recommendations[0].Strategies {
			if s.Strategy == "table" && s.Priority == "recommended" {
				tableRecommended = true
			}
		}
		if !tableRecommended {
			t.Errorf("table should be recommended for %s operation", method)
		}
	}
}

func TestSuggestStrategy_EachRecommendation_HasRationale(t *testing.T) {
	h := tools.SuggestStrategyHandler()
	input := mustMarshal(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})
	result, _ := h(context.Background(), input)
	var resp struct {
		Recommendations []struct {
			Strategies []struct {
				Strategy  string `json:"strategy"`
				Rationale string `json:"rationale"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshal(t, result, &resp)
	for _, s := range resp.Recommendations[0].Strategies {
		if s.Rationale == "" {
			t.Errorf("strategy %q has empty rationale", s.Strategy)
		}
	}
}

// --- bt_validate ---

func TestValidate_ValidConfig_ReturnsValidTrue(t *testing.T) {
	configPath := writeMinimalConfig(t)
	h := tools.ValidateHandler()
	input := mustMarshal(t, map[string]any{"config_path": configPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Valid  bool     `json:"valid"`
		Errors []any    `json:"errors"`
	}
	mustUnmarshal(t, result, &resp)
	if !resp.Valid {
		t.Errorf("expected valid=true for a valid config, errors: %v", resp.Errors)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("expected empty errors for valid config, got: %v", resp.Errors)
	}
}

func TestValidate_InvalidConfig_ReturnsValidFalseWithErrors(t *testing.T) {
	configPath := writeInvalidConfig(t)
	h := tools.ValidateHandler()
	input := mustMarshal(t, map[string]any{"config_path": configPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Valid  bool `json:"valid"`
		Errors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.Valid {
		t.Error("expected valid=false for invalid config")
	}
	if len(resp.Errors) == 0 {
		t.Error("expected at least one error entry for invalid config")
	}
}

func TestValidate_ErrorEntry_HasFieldAndMessage(t *testing.T) {
	configPath := writeInvalidConfig(t)
	h := tools.ValidateHandler()
	input := mustMarshal(t, map[string]any{"config_path": configPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		Errors []struct {
			Field   string `json:"field"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	mustUnmarshal(t, result, &resp)
	for _, e := range resp.Errors {
		if e.Field == "" {
			t.Error("error entry has empty field")
		}
		if e.Message == "" {
			t.Error("error entry has empty message")
		}
	}
}

func TestValidate_MissingFile_ReturnsValidFalse(t *testing.T) {
	h := tools.ValidateHandler()
	input := mustMarshal(t, map[string]any{"config_path": "/does/not/exist.yaml"})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured response, not Go error: %v", err)
	}
	var resp struct {
		Valid bool `json:"valid"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.Valid {
		t.Error("expected valid=false for missing config file")
	}
}

// --- bt_scaffold_config ---

func TestScaffoldConfig_ValidSchema_ReturnsYAML(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		ConfigYAML    string `json:"config_yaml"`
		WrittenToDisk bool   `json:"written_to_disk"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.ConfigYAML == "" {
		t.Error("expected non-empty config_yaml in response")
	}
	if resp.WrittenToDisk {
		t.Error("expected written_to_disk=false when no output_path given")
	}
}

func TestScaffoldConfig_GeneratedYAML_ContainsVersion(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath})
	result, _ := h(context.Background(), input)
	var resp struct{ ConfigYAML string `json:"config_yaml"` }
	mustUnmarshal(t, result, &resp)
	if !containsString(resp.ConfigYAML, "version: 1") {
		t.Error("generated config must contain 'version: 1'")
	}
}

func TestScaffoldConfig_GeneratedYAML_ContainsTargetBlock(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{"schema_path": schemaPath, "base_url": "http://localhost:9000"})
	result, _ := h(context.Background(), input)
	var resp struct{ ConfigYAML string `json:"config_yaml"` }
	mustUnmarshal(t, result, &resp)
	if !containsString(resp.ConfigYAML, "base_url") {
		t.Error("generated config must contain base_url")
	}
	if !containsString(resp.ConfigYAML, "http://localhost:9000") {
		t.Error("generated config must use the provided base_url")
	}
}

func TestScaffoldConfig_WithOutputPath_WritesFileToDisk(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	outputPath := filepath.Join(t.TempDir(), "backendtest.yaml")
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{
		"schema_path": schemaPath,
		"output_path": outputPath,
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		WrittenToDisk bool   `json:"written_to_disk"`
		OutputPath    string `json:"output_path"`
	}
	mustUnmarshal(t, result, &resp)
	if !resp.WrittenToDisk {
		t.Error("expected written_to_disk=true when output_path is provided")
	}
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		t.Error("expected file to exist at output_path after scaffold")
	}
}

func TestScaffoldConfig_GeneratedYAML_IsParseableByConfigLoader(t *testing.T) {
	schemaPath := writeMinimalOpenAPISpec(t)
	outputPath := filepath.Join(t.TempDir(), "backendtest.yaml")
	h := tools.ScaffoldConfigHandler()
	input := mustMarshal(t, map[string]any{
		"schema_path": schemaPath,
		"output_path": outputPath,
	})
	h(context.Background(), input)

	// Validate the generated config using bt_validate.
	vh := tools.ValidateHandler()
	vInput := mustMarshal(t, map[string]any{"config_path": outputPath})
	vResult, _ := vh(context.Background(), vInput)
	var vResp struct {
		Valid  bool  `json:"valid"`
		Errors []any `json:"errors"`
	}
	mustUnmarshal(t, vResult, &vResp)
	if !vResp.Valid {
		t.Errorf("generated config failed validation: %v", vResp.Errors)
	}
}

// --- bt_run ---

func TestRun_MissingConfigPath_ReturnsValidationError(t *testing.T) {
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{})
	_, err := h(context.Background(), input)
	if err == nil {
		t.Error("expected error for missing config_path")
	}
}

func TestRun_InvalidConfigPath_ReturnsStructuredError(t *testing.T) {
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{"config_path": "/no/such/config.yaml"})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured error response, not Go error: %v", err)
	}
	var resp map[string]any
	mustUnmarshal(t, result, &resp)
	if _, hasErr := resp["error"]; !hasErr {
		t.Error("expected 'error' field for invalid config path")
	}
}

func TestRun_SuccessfulRun_ResponseHasRequiredFields(t *testing.T) {
	// Use a real httptest server and a generated config.
	configPath := writeRunnableConfig(t)
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{
		"config_path": configPath,
		"strategy":    "table",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Passed      int    `json:"passed"`
		Failed      int    `json:"failed"`
		Skipped     int    `json:"skipped"`
		Total       int    `json:"total"`
		Strategy    string `json:"strategy"`
		DurationMs  int    `json:"duration_ms"`
		ArtifactDir string `json:"artifact_dir"`
		Failures    []any  `json:"failures"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.Strategy == "" {
		t.Error("response must include strategy field")
	}
	if resp.Total == 0 {
		t.Error("expected total > 0 for a run with at least one case")
	}
	if resp.ArtifactDir == "" {
		t.Error("response must include artifact_dir")
	}
	if resp.Failures == nil {
		t.Error("failures field must be present (even if empty)")
	}
}

func TestRun_FailedCase_IncludesArtifactPath(t *testing.T) {
	configPath := writeFailingConfig(t)
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{
		"config_path": configPath,
		"strategy":    "table",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Failures []struct {
			CaseID       string `json:"case_id"`
			ArtifactPath string `json:"artifact_path"`
			Summary      string `json:"summary"`
		} `json:"failures"`
	}
	mustUnmarshal(t, result, &resp)
	if len(resp.Failures) == 0 {
		t.Fatal("expected at least one failure from a failing config")
	}
	for _, f := range resp.Failures {
		if f.ArtifactPath == "" {
			t.Errorf("failure %q has no artifact_path", f.CaseID)
		}
		if f.Summary == "" {
			t.Errorf("failure %q has no summary", f.CaseID)
		}
	}
}

func TestRun_PassedCount_PlusFailedCount_EqualsTotal(t *testing.T) {
	configPath := writeRunnableConfig(t)
	h := tools.RunHandler()
	input := mustMarshal(t, map[string]any{"config_path": configPath, "strategy": "table"})
	result, _ := h(context.Background(), input)
	var resp struct {
		Passed  int `json:"passed"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
		Total   int `json:"total"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.Passed+resp.Failed+resp.Skipped != resp.Total {
		t.Errorf("passed(%d)+failed(%d)+skipped(%d) != total(%d)",
			resp.Passed, resp.Failed, resp.Skipped, resp.Total)
	}
}

// --- bt_explain_failure ---

func TestExplainFailure_ValidArtifact_ReturnsStructuredDetail(t *testing.T) {
	artifactPath := writeArtifactFile(t, sampleArtifact())
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": artifactPath})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		CaseID  string `json:"case_id"`
		Strategy string `json:"strategy"`
		Request  struct {
			Method string `json:"method"`
			URL    string `json:"url"`
		} `json:"request"`
		Response struct {
			StatusCode int `json:"status_code"`
		} `json:"response"`
		Failures []struct {
			Invariant string `json:"invariant"`
			Message   string `json:"message"`
		} `json:"failures"`
		ReplayCommand string `json:"replay_command"`
	}
	mustUnmarshal(t, result, &resp)
	if resp.CaseID != "CreateOrder" {
		t.Errorf("expected CaseID=CreateOrder, got %q", resp.CaseID)
	}
	if resp.Request.Method != "POST" {
		t.Errorf("expected request method POST, got %q", resp.Request.Method)
	}
	if resp.Response.StatusCode != 500 {
		t.Errorf("expected status code 500, got %d", resp.Response.StatusCode)
	}
	if len(resp.Failures) == 0 {
		t.Error("expected at least one failure in artifact detail")
	}
	if resp.ReplayCommand == "" {
		t.Error("expected replay_command to be set")
	}
}

func TestExplainFailure_ReplayCommand_ContainsBtReplay(t *testing.T) {
	artifactPath := writeArtifactFile(t, sampleArtifact())
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": artifactPath})
	result, _ := h(context.Background(), input)
	var resp struct{ ReplayCommand string `json:"replay_command"` }
	mustUnmarshal(t, result, &resp)
	if !containsString(resp.ReplayCommand, "bt replay") {
		t.Errorf("expected replay_command to contain 'bt replay', got %q", resp.ReplayCommand)
	}
}

func TestExplainFailure_ReplayCommand_ContainsArtifactPath(t *testing.T) {
	artifactPath := writeArtifactFile(t, sampleArtifact())
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": artifactPath})
	result, _ := h(context.Background(), input)
	var resp struct{ ReplayCommand string `json:"replay_command"` }
	mustUnmarshal(t, result, &resp)
	if !containsString(resp.ReplayCommand, artifactPath) {
		t.Errorf("expected replay_command to contain artifact path %q, got %q", artifactPath, resp.ReplayCommand)
	}
}

func TestExplainFailure_MissingArtifact_ReturnsStructuredError(t *testing.T) {
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": "/does/not/exist.json"})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured error response, not Go error: %v", err)
	}
	var resp map[string]any
	mustUnmarshal(t, result, &resp)
	if resp["code"] != "ARTIFACT_NOT_FOUND" {
		t.Errorf("expected code ARTIFACT_NOT_FOUND, got %v", resp["code"])
	}
}

func TestExplainFailure_FailureEntries_HaveInvariantAndMessage(t *testing.T) {
	artifactPath := writeArtifactFile(t, sampleArtifact())
	h := tools.ExplainFailureHandler()
	input := mustMarshal(t, map[string]any{"artifact_path": artifactPath})
	result, _ := h(context.Background(), input)
	var resp struct {
		Failures []struct {
			Invariant string `json:"invariant"`
			Message   string `json:"message"`
		} `json:"failures"`
	}
	mustUnmarshal(t, result, &resp)
	for _, f := range resp.Failures {
		if f.Invariant == "" {
			t.Error("failure entry has empty invariant")
		}
		if f.Message == "" {
			t.Error("failure entry has empty message")
		}
	}
}

// --- Test file helpers ---

func writeMinimalOpenAPISpec(t *testing.T) string {
	t.Helper()
	spec := `openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths:
  /health:
    get:
      operationId: GetHealth
      responses:
        "200":
          description: OK
  /orders:
    post:
      operationId: CreateOrder
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              required: [amount, currency]
              properties:
                amount:
                  type: number
                currency:
                  type: string
      responses:
        "201":
          description: Created
`
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(path, []byte(spec), 0644); err != nil {
		t.Fatalf("cannot write openapi spec: %v", err)
	}
	return path
}

func writeMinimalConfig(t *testing.T) string {
	t.Helper()
	schemaPath := writeMinimalOpenAPISpec(t)
	cfg := `version: 1
target:
  name: test-api
  base_url: http://localhost:8080
  schema: ` + schemaPath + `
strategies:
  - type: table
    file: ./cases/table.yaml
safety:
  profile: safe
`
	path := filepath.Join(t.TempDir(), "backendtest.yaml")
	os.WriteFile(path, []byte(cfg), 0644)
	return path
}

func writeInvalidConfig(t *testing.T) string {
	t.Helper()
	// Missing required 'target' block.
	cfg := `version: 1
strategies:
  - type: table
`
	path := filepath.Join(t.TempDir(), "backendtest-invalid.yaml")
	os.WriteFile(path, []byte(cfg), 0644)
	return path
}

func writeArtifactFile(t *testing.T, a model.Artifact) string {
	t.Helper()
	dir := t.TempDir()
	data, _ := json.MarshalIndent(a, "", "  ")
	path := filepath.Join(dir, "artifact.json")
	os.WriteFile(path, data, 0644)
	return path
}

func containsString(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (haystack == needle ||
		len(needle) == 0 ||
		func() bool {
			for i := 0; i <= len(haystack)-len(needle); i++ {
				if haystack[i:i+len(needle)] == needle {
					return true
				}
			}
			return false
		}())
}
```

---

## Step 3 — `bt mcp serve` subcommand

### Spec

- `bt mcp serve` starts a long-running MCP protocol server on stdio transport
- The server uses the [MCP Go SDK](https://github.com/mark3labs/mcp-go) or equivalent — the transport layer must not be hand-rolled
- All six tools are registered at server start
- The server logs to stderr (not stdout — stdout is the MCP transport)
- On `SIGINT` or `SIGTERM`, the server shuts down cleanly: in-flight tool calls are allowed to complete, no new calls are accepted
- `bt mcp serve --config <path>` sets the default config path for tool calls that omit `config_path` (providing a sensible default)
- Exit code 0 on clean shutdown, 2 on startup failure

### Tests

`internal/cli/mcp_serve_test.go`:

```go
package cli_test

import (
	"bytes"
	"testing"

	"github.com/jimbery/bt/internal/cli"
)

func TestMCPServeCommand_ExistsAsSubcommand(t *testing.T) {
	cmd := cli.NewRootCmd()
	mcpCmd, _, err := cmd.Find([]string{"mcp", "serve"})
	if err != nil || mcpCmd == nil {
		t.Error("expected 'bt mcp serve' to exist as a subcommand")
	}
}

func TestMCPServeCommand_ConfigFlagExists(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "serve", "--help"})
	cmd.Execute()
	if !bytes.Contains(buf.Bytes(), []byte("--config")) {
		t.Error("expected --config flag in 'bt mcp serve --help'")
	}
}

func TestMCPServeCommand_AppearsInRootHelp(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})
	cmd.Execute()
	if !bytes.Contains(buf.Bytes(), []byte("mcp")) {
		t.Error("expected 'mcp' to appear in root command help")
	}
}
```

---

## Step 4 — Tool description quality

### Spec

Every tool description must meet these requirements, verified by test:
- Non-empty
- At least 20 characters (trivially short descriptions are not useful to an AI client)
- Does not contain implementation details (package names, function names, Go types)
- Names any prerequisite tool calls (e.g. `bt_run` mentions that `bt_explain_failure` can follow)
- Uses the tool's own name in the description

### Tests

`internal/mcp/tools/descriptions_test.go`:

```go
package tools_test

import (
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/mcp/tools"
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
		json.Unmarshal(tool.InputSchema, &schema)
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
```

---

## Local verification

```bash
# Unit tests
go test ./internal/mcp/... -race -v
go test ./internal/cli/... -race -v

# Build and verify the subcommand exists
go build -o bt ./cmd/bt
./bt mcp serve --help
./bt --help | grep mcp

# Smoke test individual tools via the CLI JSON output
./bt validate --config examples/orders-api/bt/backendtest.yaml --output json

# Start the MCP server (it blocks waiting for stdio input)
./bt mcp serve --config examples/orders-api/bt/backendtest.yaml
```

---

## Model additions required

Before implementation, confirm these fields exist in `pkg/model/`:

| Type | Field | Purpose |
|---|---|---|
| `model.Artifact` | All existing fields from M3 | `bt_explain_failure` reads the full artifact |
| `model.RunConfig` | `ArtifactDir string` | `bt_run` tool returns the artifact directory |

No new model fields are required for M6 — it is a presentation layer over the existing engine.

---

## M6 exit criterion

Any MCP-compatible client can:
1. Call `bt_discover_operations` with a schema path and receive a structured list of operations
2. Call `bt_suggest_strategy` with those operations and receive prioritised strategy recommendations
3. Call `bt_validate` with a config path and receive pass/fail with error detail
4. Call `bt_scaffold_config` and receive a valid `backendtest.yaml` as a string
5. Call `bt_run` and receive a compact summary with artifact paths for any failures
6. Call `bt_explain_failure` with an artifact path and receive full request/response detail and a ready-to-run replay command

All tools return structured errors (not Go errors) for bad inputs. All unit tests pass with `-race`.