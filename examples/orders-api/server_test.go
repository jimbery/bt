package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})))
	os.Exit(m.Run())
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(NewRouter())
}

func TestHealth_Returns200(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestHealth_ResponseShape(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("cannot decode response: %v", err)
	}
	if body["status"] == nil {
		t.Error("expected 'status' field in health response")
	}
}

func TestListOrders_Returns200(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/orders")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestListOrders_ReturnsArray(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/orders")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("cannot decode response: %v", err)
	}
	if body["orders"] == nil {
		t.Error("expected 'orders' field in list response")
	}
}

func TestListOrders_StatusFilter(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/orders?status=pending")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for status filter, got %d", resp.StatusCode)
	}
}

func TestListOrders_InvalidStatusFilter(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/orders?status=invalid_status")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid status, got %d", resp.StatusCode)
	}
}

func TestCreateOrder_Returns201(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"amount": 100, "currency": "GBP", "description": "Test order"}`
	resp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}
}

func TestCreateOrder_ResponseContainsID(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"amount": 100, "currency": "GBP"}`
	resp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var created map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("cannot decode response: %v", err)
	}
	if created["id"] == nil {
		t.Error("expected 'id' in create response")
	}
}

func TestCreateOrder_MissingAmount_Returns400(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"currency": "GBP"}`
	resp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing amount, got %d", resp.StatusCode)
	}
}

func TestCreateOrder_InvalidAmount_Returns400(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"amount": -1, "currency": "GBP"}`
	resp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for negative amount, got %d", resp.StatusCode)
	}
}

func TestCreateOrder_MissingCurrency_Returns400(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"amount": 100}`
	resp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing currency, got %d", resp.StatusCode)
	}
}

func TestCreateOrder_InvalidBody_Returns400(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString("not json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", resp.StatusCode)
	}
}

func TestGetOrder_ExistingOrder_Returns200(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"amount": 100, "currency": "GBP"}`
	createResp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("unexpected error creating order: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()

	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("cannot decode create response: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected string id in create response")
	}

	resp, err := http.Get(srv.URL + "/orders/" + id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestGetOrder_NonExistentOrder_Returns404(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/orders/does-not-exist")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
}

func TestUpdateOrder_ValidStatus_Returns200(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"amount": 100, "currency": "GBP"}`
	createResp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()

	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected id")
	}

	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/orders/"+id,
		bytes.NewBufferString(`{"status": "confirmed"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestUpdateOrder_InvalidStatus_Returns400(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"amount": 100, "currency": "GBP"}`
	createResp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()

	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected id")
	}

	req, err := http.NewRequest(http.MethodPatch, srv.URL+"/orders/"+id,
		bytes.NewBufferString(`{"status": "not_a_valid_status"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid status, got %d", resp.StatusCode)
	}
}

func TestBrokenEndpoint_IsNotConsistent(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := `{"amount": 100, "currency": "GBP"}`
	createResp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected id")
	}

	resp, err := http.Get(srv.URL + "/orders/" + id + "/broken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode == 0 {
		t.Error("expected a non-zero status code")
	}
}

func TestBrokenEndpoint_NonExistentID_StillResponds200(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/orders/does-not-exist/broken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from broken endpoint for unknown ID, got %d", resp.StatusCode)
	}
}

func TestDeleteOrder_WithConfirmHeader_Returns204(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	body := bytes.NewBufferString(`{"amount":10,"currency":"GBP"}`)
	createResp, err := http.Post(srv.URL+"/orders", "application/json", body)
	if err != nil {
		t.Fatalf("cannot create order: %v", err)
	}
	defer func() { _ = createResp.Body.Close() }()
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected id in create response")
	}

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/orders/"+id, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Confirm-Delete", "yes")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("expected 204, got %d", resp.StatusCode)
	}
}

func TestDeleteOrder_WithoutConfirmHeader_Returns400(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/orders/ord-001", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 without confirm header, got %d", resp.StatusCode)
	}

	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if respBody["code"] != "MISSING_CONFIRM" {
		t.Errorf("expected error code MISSING_CONFIRM, got %v", respBody["code"])
	}
}

func TestDeleteOrder_Returns400_ResponseSchema(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/orders/anything", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var respBody map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&respBody); err != nil {
		t.Fatalf("response body is not valid JSON: %v", err)
	}
	if _, ok := respBody["error"].(string); !ok {
		t.Error("expected 'error' string field in 400 response")
	}
	if _, ok := respBody["code"].(string); !ok {
		t.Error("expected 'code' string field in 400 response")
	}
}

func TestAdminDeleteCount_ReflectsDeleteAttempts(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/admin/delete-count")
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	var initial map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&initial); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp.Body.Close()
	startCount := int(initial["delete_attempts"].(float64))

	req, err := http.NewRequest(http.MethodDelete, srv.URL+"/orders/ord-001", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = http.DefaultClient.Do(req)

	resp2, err := http.Get(srv.URL + "/admin/delete-count")
	if err != nil {
		t.Fatalf("admin: %v", err)
	}
	var after map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&after); err != nil {
		t.Fatalf("decode: %v", err)
	}
	_ = resp2.Body.Close()
	endCount := int(after["delete_attempts"].(float64))

	if endCount != startCount+1 {
		t.Errorf("expected delete_attempts to increase by 1: start=%d end=%d", startCount, endCount)
	}
}

func TestAdminDeleteCount_ResponseSchema(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/admin/delete-count")
	if err != nil {
		t.Fatalf("cannot reach /admin/delete-count: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 from /admin/delete-count, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	count, ok := body["delete_attempts"]
	if !ok {
		t.Error("expected 'delete_attempts' field in response")
	}
	if _, ok := count.(float64); !ok {
		t.Errorf("expected 'delete_attempts' to be a number, got %T", count)
	}
}

func TestAllEndpoints_ReturnJSONContentType(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	endpoints := []struct {
		method string
		path   string
		body   io.Reader
	}{
		{"GET", "/health", nil},
		{"GET", "/orders", nil},
		{"POST", "/orders", bytes.NewBufferString(`{"amount":100,"currency":"GBP"}`)},
	}

	for _, ep := range endpoints {
		req, err := http.NewRequest(ep.method, srv.URL+ep.path, ep.body)
		if err != nil {
			t.Fatal(err)
		}
		if ep.body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("%s %s: unexpected error: %v", ep.method, ep.path, err)
		}
		_ = resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		if ct == "" {
			t.Errorf("%s %s: expected Content-Type header, got none", ep.method, ep.path)
		}
	}
}
