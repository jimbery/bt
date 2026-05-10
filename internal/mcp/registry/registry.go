package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ErrUnknownTool is returned when Dispatch is called with a name that was never registered.
var ErrUnknownTool = errors.New("unknown tool")

// HandlerFunc is invoked after input JSON validates against the tool's InputSchema.
type HandlerFunc func(ctx context.Context, input json.RawMessage) (json.RawMessage, error)

// Tool describes a single MCP tool.
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Handler     HandlerFunc
}

// ToolSummary is a lightweight listing entry for MCP clients.
type ToolSummary struct {
	Name        string
	Description string
}

// Registry maps tool names to handlers and validates inputs with JSON Schema.
// It is safe for concurrent reads; Register must not be used concurrently with Dispatch.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]compiledTool
}

type compiledTool struct {
	tool Tool
	sch  *jsonschema.Schema
}

// New returns an empty registry.
func New() *Registry {
	return &Registry{tools: make(map[string]compiledTool)}
}

// Register adds a tool. It returns an error if the name is empty, duplicate, description
// is empty, the handler is nil, or the input schema is not valid JSON Schema.
func (r *Registry) Register(tool Tool) error {
	if tool.Name == "" {
		return errors.New("tool name is empty")
	}
	if tool.Description == "" {
		return errors.New("tool description is empty")
	}
	if tool.Handler == nil {
		return errors.New("tool handler is nil")
	}
	if len(bytes.TrimSpace(tool.InputSchema)) == 0 {
		return errors.New("tool InputSchema is empty")
	}
	if !json.Valid(tool.InputSchema) {
		return errors.New("tool InputSchema is not valid JSON")
	}

	schemaDoc, err := jsonschema.UnmarshalJSON(bytes.NewReader(tool.InputSchema))
	if err != nil {
		return fmt.Errorf("parse InputSchema: %w", err)
	}

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	uri := "https://bt.local/mcp/schema/" + url.PathEscape(tool.Name) + ".json"
	if err := c.AddResource(uri, schemaDoc); err != nil {
		return fmt.Errorf("add InputSchema resource: %w", err)
	}
	sch, err := c.Compile(uri)
	if err != nil {
		return fmt.Errorf("compile InputSchema: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[tool.Name]; exists {
		return fmt.Errorf("duplicate tool name %q", tool.Name)
	}
	r.tools[tool.Name] = compiledTool{tool: tool, sch: sch}
	return nil
}

// Get returns the tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.tools[name]
	if !ok {
		return Tool{}, false
	}
	return e.tool, true
}

// List returns name and description for every tool, sorted by name.
func (r *Registry) List() []ToolSummary {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.tools))
	for n := range r.tools {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]ToolSummary, 0, len(names))
	for _, n := range names {
		t := r.tools[n].tool
		out = append(out, ToolSummary{Name: t.Name, Description: t.Description})
	}
	return out
}

// Dispatch validates input JSON, then invokes the handler. Unknown tools return ErrUnknownTool.
// Handler panics are recovered and returned as an error.
func (r *Registry) Dispatch(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	r.mu.RLock()
	e, ok := r.tools[name]
	r.mu.RUnlock()
	if !ok {
		return nil, ErrUnknownTool
	}

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(input))
	if err != nil {
		return nil, fmt.Errorf("invalid input JSON: %w", err)
	}
	if err := e.sch.Validate(inst); err != nil {
		return nil, fmt.Errorf("input does not match tool schema: %w", err)
	}

	var out json.RawMessage
	var handlerErr error
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				handlerErr = fmt.Errorf("handler panic: %v", rec)
			}
		}()
		out, handlerErr = e.tool.Handler(ctx, input)
	}()

	return out, handlerErr
}
