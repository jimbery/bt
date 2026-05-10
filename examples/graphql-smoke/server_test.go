package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGraphqlPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(graphql))
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(`{"query":"{ ping }"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	data, _ := out["data"].(map[string]any)
	if data["ping"] != "ok" {
		t.Fatalf("got %#v", out)
	}
}

func TestGraphqlWidgetAndCreate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(graphql))
	defer srv.Close()
	client := srv.Client()

	payload := map[string]any{
		"query":     `query W($id: ID!) { widget(id: $id) { id name } }`,
		"variables": map[string]any{"id": "w1"},
	}
	b, _ := json.Marshal(payload)
	resp, err := client.Post(srv.URL, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Contains(body, []byte(`"id"`)) || !bytes.Contains(body, []byte(`"seed"`)) {
		t.Fatalf("widget response: %s", body)
	}

	payload2 := map[string]any{
		"query":     `mutation M($name: String!) { createWidget(name: $name) { id name } }`,
		"variables": map[string]any{"name": "x"},
	}
	b2, _ := json.Marshal(payload2)
	resp2, err := client.Post(srv.URL, "application/json", bytes.NewReader(b2))
	if err != nil {
		t.Fatal(err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	if !bytes.Contains(body2, []byte(`"name"`)) || !bytes.Contains(body2, []byte(`"x"`)) {
		t.Fatalf("create response: %s", body2)
	}
}
