package main_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	ordersapi "github.com/jimbery/bt/examples/orders-api"
)

func createOrder(t *testing.T, srv *httptest.Server, amount int, currency string) string {
	t.Helper()
	body := fmt.Sprintf(`{"amount":%d,"currency":"%s"}`, amount, currency)
	resp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("createOrder: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatalf("createOrder: expected 201, got %d", resp.StatusCode)
	}
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("createOrder decode: %v", err)
	}
	id, ok := result["id"].(string)
	if !ok || id == "" {
		t.Fatalf("createOrder: no id in response: %v", result)
	}
	return id
}

func TestFlow_CreateAndRetrieve_BindingPropagates(t *testing.T) {
	srv := httptest.NewServer(ordersapi.NewRouter())
	defer srv.Close()

	id := createOrder(t, srv, 100, "GBP")

	resp, err := http.Get(srv.URL + "/orders/" + id)
	if err != nil {
		t.Fatalf("GET /orders/%s: %v", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("retrieve returns 200: want 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["id"] != id {
		t.Errorf("retrieved order id matches created id: want %q, got %v", id, body["id"])
	}
	if body["status"] != "pending" {
		t.Errorf("retrieved order status is pending: want 'pending', got %v", body["status"])
	}
	for _, field := range []string{"id", "amount", "currency", "status", "created_at"} {
		if body[field] == nil {
			t.Errorf("required field %q missing from retrieved order", field)
		}
	}
}

func TestFlow_CreateAndUpdate_StatusChanges(t *testing.T) {
	srv := httptest.NewServer(ordersapi.NewRouter())
	defer srv.Close()

	id := createOrder(t, srv, 50, "USD")

	patchBody := `{"status":"confirmed"}`
	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/orders/"+id, bytes.NewBufferString(patchBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /orders/%s: %v", id, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("patch returns 200: want 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body["status"] != "confirmed" {
		t.Errorf("updated status is confirmed: want 'confirmed', got %v", body["status"])
	}
	if body["id"] != id {
		t.Errorf("id is unchanged after update: want %q, got %v", id, body["id"])
	}
}

func TestFlow_CreateAndCancel_CancelledAtPresent(t *testing.T) {
	srv := httptest.NewServer(ordersapi.NewRouter())
	defer srv.Close()

	id := createOrder(t, srv, 200, "EUR")

	patchBody := `{"status":"cancelled"}`
	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/orders/"+id, bytes.NewBufferString(patchBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /orders/%s: %v", id, err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if body["status"] != "cancelled" {
		t.Errorf("cancelled status is set: want 'cancelled', got %v", body["status"])
	}
	cancelledAt, ok := body["cancelled_at"].(string)
	if !ok || cancelledAt == "" {
		t.Errorf("cancelled_at is present and is a string: got %v (%T)", body["cancelled_at"], body["cancelled_at"])
	}
	if cancelledAt != "" {
		if _, err := time.Parse(time.RFC3339, cancelledAt); err != nil {
			t.Errorf("cancelled_at %q is not RFC3339: %v", cancelledAt, err)
		}
	}
}
