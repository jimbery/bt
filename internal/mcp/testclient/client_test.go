package testclient_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jayimbery/bt/internal/mcp/testclient"
	"github.com/jayimbery/bt/internal/testutil"
)

func btBinary(t *testing.T) string {
	t.Helper()
	root := testutil.RepoRoot(t)
	p := filepath.Join(root, "bt")
	if _, err := os.Stat(p); err != nil {
		t.Skip("bt binary not found; build with: go build -o bt ./cmd/bt")
	}
	return p
}

func TestClient_Start_ConnectsToServer(t *testing.T) {
	client, err := testclient.Start(context.Background(), btBinary(t), "backendtest.yaml")
	if err != nil {
		t.Fatalf("failed to start MCP client: %v", err)
	}
	defer client.Close()
}

func TestClient_Close_IsIdempotent(t *testing.T) {
	client, err := testclient.Start(context.Background(), btBinary(t), "backendtest.yaml")
	if err != nil {
		t.Fatalf("failed to start MCP client: %v", err)
	}
	_ = client.Close()
	_ = client.Close()
}

func TestClient_Call_UnknownTool_ReturnsError(t *testing.T) {
	client, err := testclient.Start(context.Background(), btBinary(t), "backendtest.yaml")
	if err != nil {
		t.Fatalf("failed to start MCP client: %v", err)
	}
	defer client.Close()

	_, err = client.Call(context.Background(), "bt_does_not_exist", map[string]any{})
	if err == nil {
		t.Error("expected error for unknown tool")
	}
}

func TestClient_Call_AfterClose_ReturnsError(t *testing.T) {
	client, err := testclient.Start(context.Background(), btBinary(t), "backendtest.yaml")
	if err != nil {
		t.Fatalf("failed to start MCP client: %v", err)
	}
	_ = client.Close()

	_, err = client.Call(context.Background(), "bt_validate", map[string]any{"config_path": "."})
	if err == nil {
		t.Error("expected error when calling after close")
	}
}
