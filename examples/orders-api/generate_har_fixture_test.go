//go:build generate_fixture

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/testutil"
)

// TestGenerateTraceSampleHAR records scripted traffic against NewRouter() and writes
// examples/orders-api/bt/trace/sample.har (HAR 1.2) for M12.5 trace integration tests.
func TestGenerateTraceSampleHAR(t *testing.T) {
	root := testutil.RepoRoot(t)
	outPath := filepath.Join(root, "examples/orders-api/bt/trace/sample.har")

	srv := httptest.NewServer(NewRouter())
	defer srv.Close()
	client := srv.Client()

	base := time.Date(2024, 6, 15, 10, 0, 0, 0, time.UTC)
	seq := 0
	next := func() time.Time {
		t := base.Add(time.Duration(seq) * time.Millisecond)
		seq++
		return t
	}

	var entries []map[string]any

	doPost := func(reqBody string) string {
		started := next()
		resp, err := client.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(reqBody))
		if err != nil {
			t.Fatalf("POST /orders: %v", err)
		}
		rb, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		entries = append(entries, harJSONEntry(started, "POST", srv.URL+"/orders", reqBody, resp.StatusCode, string(rb)))
		var parsed map[string]any
		if err := json.Unmarshal(rb, &parsed); err != nil {
			t.Fatalf("decode create response: %v", err)
		}
		id, _ := parsed["id"].(string)
		if id == "" {
			t.Fatalf("expected id in response: %s", rb)
		}
		return id
	}

	doGet := func(path string) {
		started := next()
		resp, err := client.Get(srv.URL + path)
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		rb, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		entries = append(entries, harJSONEntry(started, "GET", srv.URL+path, "", resp.StatusCode, string(rb)))
	}

	var ids []string
	for i := 0; i < 90; i++ {
		var cur string
		switch {
		case i < 70:
			cur = "GBP"
		case i < 90:
			cur = "USD"
		default:
			cur = "EUR"
		}
		body := fmt.Sprintf(`{"amount":%d,"currency":%q}`, 100+i, cur)
		ids = append(ids, doPost(body))
		doGet("/orders/" + ids[i])
	}
	for i := 90; i < 100; i++ {
		body := fmt.Sprintf(`{"amount":%d,"currency":"EUR"}`, 100+i)
		ids = append(ids, doPost(body))
	}

	for i := 0; i < 20; i++ {
		doGet("/orders?status=pending")
	}

	doc := map[string]any{
		"log": map[string]any{
			"version": "1.2",
			"entries": entries,
		},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s (%d entries)", outPath, len(entries))
}

func harJSONEntry(started time.Time, method, fullURL, reqJSON string, status int, respJSON string) map[string]any {
	req := map[string]any{
		"method": method,
		"url":    fullURL,
		"headers": []any{
			map[string]any{"name": "Content-Type", "value": "application/json"},
		},
	}
	if reqJSON != "" {
		req["postData"] = map[string]any{
			"mimeType": "application/json",
			"text":     reqJSON,
		}
	}
	return map[string]any{
		"startedDateTime": started.UTC().Format(time.RFC3339Nano),
		"time":            1.0,
		"request":         req,
		"response": map[string]any{
			"status": status,
			"headers": []any{
				map[string]any{"name": "Content-Type", "value": "application/json"},
			},
			"content": map[string]any{
				"mimeType": "application/json",
				"text":     respJSON,
			},
		},
	}
}
