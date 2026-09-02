package registry_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jimbery/bt/internal/mcp/registry"
)

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
	_ = r.Register(simpleTool("bt_test"))
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

func TestRegistry_Get_KnownTool_ReturnsTool(t *testing.T) {
	r := registry.New()
	_ = r.Register(simpleTool("bt_run"))
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

func TestRegistry_List_ReturnsAllRegisteredTools(t *testing.T) {
	r := registry.New()
	_ = r.Register(simpleTool("bt_run"))
	_ = r.Register(simpleTool("bt_validate"))
	_ = r.Register(simpleTool("bt_discover_operations"))

	summaries := r.List()
	if len(summaries) != 3 {
		t.Errorf("expected 3 tool summaries, got %d", len(summaries))
	}
}

func TestRegistry_List_IsSortedAlphabetically(t *testing.T) {
	r := registry.New()
	_ = r.Register(simpleTool("bt_validate"))
	_ = r.Register(simpleTool("bt_run"))
	_ = r.Register(simpleTool("bt_discover_operations"))

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
	_ = r.Register(tool)

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
	if !errors.Is(err, registry.ErrUnknownTool) {
		t.Errorf("expected ErrUnknownTool, got %v", err)
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
	_ = r.Register(tool)

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
	_ = r.Register(tool)

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
	_ = r.Register(tool)

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
	_ = r.Register(simpleTool("bt_test"))
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
	_ = r.Register(tool)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := r.Dispatch(ctx, "bt_slow", json.RawMessage(`{}`))
	if err == nil {
		t.Error("expected context cancellation to propagate from handler")
	}
}
