package runner_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/runner"
	"github.com/jayimbery/bt/pkg/model"
)

func TestRunner_GetRequest_ReturnsResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ord-1"}`))
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	resp, err := r.Run(context.Background(), model.CaseInput{
		Method: "GET",
		Path:   "/orders/1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("StatusCode: got %d, want 200", resp.StatusCode)
	}
	if string(resp.Body) != `{"id":"ord-1"}` {
		t.Errorf("Body: got %s, want %s", resp.Body, `{"id":"ord-1"}`)
	}
}

func TestRunner_PostRequest_SendsJSONBody(t *testing.T) {
	var received map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	_, err := r.Run(context.Background(), model.CaseInput{
		Method: "POST",
		Path:   "/orders",
		Body:   map[string]any{"amount": 100},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if received["amount"] == nil {
		t.Error("expected body to be received by server")
	}
}

func TestRunner_SetsRequestHeaders(t *testing.T) {
	var receivedHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeader = r.Header.Get("X-Idempotency-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	_, err := r.Run(context.Background(), model.CaseInput{
		Method:  "POST",
		Path:    "/orders",
		Headers: map[string]string{"X-Idempotency-Key": "abc-123"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedHeader != "abc-123" {
		t.Errorf("X-Idempotency-Key: got %q, want %q", receivedHeader, "abc-123")
	}
}

func TestRunner_ReturnsResponseHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-ID", "req-xyz")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	resp, err := r.Run(context.Background(), model.CaseInput{Method: "GET", Path: "/"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Headers["X-Request-Id"] != "req-xyz" {
		t.Errorf("X-Request-Id: got %q, want req-xyz", resp.Headers["X-Request-Id"])
	}
}

func TestRunner_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := r.Run(ctx, model.CaseInput{Method: "GET", Path: "/"})
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
}

func TestRunner_Non2xxStatusIsNotAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	resp, err := r.Run(context.Background(), model.CaseInput{Method: "GET", Path: "/missing"})
	if err != nil {
		t.Fatalf("unexpected error for 404: %v", err)
	}
	if resp.StatusCode != 404 {
		t.Errorf("StatusCode: got %d, want 404", resp.StatusCode)
	}
}

func TestRunner_QueryParams_AppendedToURL(t *testing.T) {
	var receivedQuery string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := runner.New(runner.Config{BaseURL: server.URL, Timeout: 5 * time.Second})

	_, err := r.Run(context.Background(), model.CaseInput{
		Method: "GET",
		Path:   "/orders",
		Query:  map[string]string{"status": "pending"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedQuery != "status=pending" {
		t.Errorf("query string: got %q, want %q", receivedQuery, "status=pending")
	}
}
