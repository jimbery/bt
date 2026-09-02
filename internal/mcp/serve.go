package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/jimbery/bt/internal/ai"
	"github.com/jimbery/bt/internal/mcp/registry"
	"github.com/jimbery/bt/internal/mcp/tools"
)

// ServeStdio runs the MCP protocol on stdin/stdout. defaultConfigPath is used when
// bt_run or bt_validate arguments omit config_path.
func ServeStdio(ctx context.Context, defaultConfigPath string, errLog io.Writer) error {
	if errLog == nil {
		errLog = io.Discard
	}
	logger := log.New(errLog, "", log.LstdFlags)

	pc, err := ai.LoadProviderConfig()
	if err != nil {
		return fmt.Errorf("ai config: %w", err)
	}
	provider, err := ai.NewProvider(pc)
	if err != nil {
		return fmt.Errorf("ai provider: %w", err)
	}

	reg := registry.New()
	for _, t := range tools.AllWithProvider(provider) {
		if err := reg.Register(t); err != nil {
			return fmt.Errorf("register tool %q: %w", t.Name, err)
		}
	}

	srv := mcpserver.NewMCPServer("bt", "1.0.0")

	for _, def := range tools.AllWithProvider(provider) {
		toolName := def.Name
		mcpTool := mcp.NewToolWithRawSchema(def.Name, def.Description, def.InputSchema)
		srv.AddTool(mcpTool, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			name := toolName
			args := req.GetArguments()
			raw, err := json.Marshal(args)
			if err != nil {
				return mcp.NewToolResultErrorf("marshal arguments: %v", err), nil
			}
			if defaultConfigPath != "" && (name == "bt_run" || name == "bt_validate") {
				var m map[string]any
				if err := json.Unmarshal(raw, &m); err == nil {
					if _, ok := m["config_path"]; !ok || m["config_path"] == nil || fmt.Sprint(m["config_path"]) == "" {
						m["config_path"] = defaultConfigPath
					}
					raw, _ = json.Marshal(m)
				}
			}
			out, err := reg.Dispatch(ctx, name, raw)
			if err != nil {
				return mcp.NewToolResultErrorFromErr("tool error", err), nil
			}
			var structured any
			if err := json.Unmarshal(out, &structured); err != nil {
				return mcp.NewToolResultText(string(out)), nil
			}
			return mcp.NewToolResultStructuredOnly(structured), nil
		})
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	stdioSrv := mcpserver.NewStdioServer(srv)
	stdioSrv.SetErrorLogger(logger)
	logger.Println("bt MCP server listening on stdio")
	if err := stdioSrv.Listen(ctx, os.Stdin, os.Stdout); err != nil {
		return fmt.Errorf("stdio listen: %w", err)
	}
	return nil
}
