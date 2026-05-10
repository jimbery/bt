// Package testclient provides a minimal stdio MCP client for integration tests.
package testclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/mark3labs/mcp-go/mcp"
)

// Client drives bt mcp serve over stdio. It is not safe for concurrent use.
type Client struct {
	mu     sync.Mutex
	closed bool
	inner  *mcpclient.Client
}

// Start launches bt mcp serve as a subprocess, performs MCP initialize, and returns a ready client.
// serveDefaultConfig is passed to mcp serve --config (tools may still override config_path in inputs).
func Start(ctx context.Context, btPath, serveDefaultConfig string) (*Client, error) {
	if strings.TrimSpace(btPath) == "" {
		return nil, errors.New("bt path is empty")
	}
	if serveDefaultConfig == "" {
		serveDefaultConfig = "backendtest.yaml"
	}
	args := []string{"mcp", "serve", "--config", serveDefaultConfig}

	var opts []transport.StdioOption
	if d := filepath.Dir(btPath); d != "" {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			opts = append(opts, transport.WithCommandFunc(func(ctx context.Context, command string, env []string, args []string) (*exec.Cmd, error) {
				cmd := exec.CommandContext(ctx, command, args...)
				cmd.Dir = d
				cmd.Env = append(os.Environ(), env...)
				return cmd, nil
			}))
		}
	}

	inner, err := mcpclient.NewStdioMCPClientWithOptions(btPath, nil, args, opts...)
	if err != nil {
		return nil, fmt.Errorf("start stdio mcp: %w", err)
	}
	if err := inner.Start(ctx); err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("mcp client start: %w", err)
	}
	init := mcp.InitializeRequest{}
	init.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	init.Params.ClientInfo = mcp.Implementation{Name: "bt-mcp-testclient", Version: "1.0.0"}
	init.Params.Capabilities = mcp.ClientCapabilities{}
	if _, err := inner.Initialize(ctx, init); err != nil {
		_ = inner.Close()
		return nil, fmt.Errorf("mcp initialize: %w", err)
	}
	return &Client{inner: inner}, nil
}

// Close shuts down the subprocess. It is safe to call more than once.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inner == nil {
		return nil
	}
	err := c.inner.Close()
	c.inner = nil
	c.closed = true
	return err
}

// Call invokes a registered MCP tool and returns the structured JSON payload (not the MCP envelope).
func (c *Client) Call(ctx context.Context, tool string, input any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.inner == nil {
		return nil, errors.New("client is closed")
	}
	args, err := argumentsMap(input)
	if err != nil {
		return nil, err
	}
	req := mcp.CallToolRequest{}
	req.Params.Name = tool
	req.Params.Arguments = args

	res, err := c.inner.CallTool(ctx, req)
	if err != nil {
		return nil, err
	}
	return callToolResultJSON(res)
}

func argumentsMap(input any) (map[string]any, error) {
	if input == nil {
		return map[string]any{}, nil
	}
	switch v := input.(type) {
	case map[string]any:
		return v, nil
	default:
		data, err := json.Marshal(input)
		if err != nil {
			return nil, fmt.Errorf("marshal tool input: %w", err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("tool input must be JSON object: %w", err)
		}
		return m, nil
	}
}

func callToolResultJSON(res *mcp.CallToolResult) (json.RawMessage, error) {
	if res.IsError {
		return nil, errors.New(toolErrorText(res))
	}
	if res.StructuredContent != nil {
		return json.Marshal(res.StructuredContent)
	}
	txt := toolTextContent(res)
	if txt != "" {
		// Prefer returning raw JSON if the tool only returned text JSON.
		if json.Valid([]byte(txt)) {
			return json.RawMessage(txt), nil
		}
		return json.Marshal(map[string]any{"text": txt})
	}
	return nil, errors.New("empty tool result")
}

func toolErrorText(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	if len(parts) == 0 {
		return "tool returned error"
	}
	return strings.Join(parts, "; ")
}

func toolTextContent(res *mcp.CallToolResult) string {
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(mcp.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "")
}
