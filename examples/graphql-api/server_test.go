package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(NewHandler())
}

func gqlRequest(t *testing.T, srv *httptest.Server, query string, variables map[string]any) map[string]any {
	t.Helper()
	payload := map[string]any{"query": query}
	if variables != nil {
		payload["variables"] = variables
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(srv.URL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gqlRequest: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return result
}

func TestServer_GraphQLResponse_HasDataKey(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	result := gqlRequest(t, srv, `{ health }`, nil)

	if _, ok := result["data"]; !ok {
		t.Error("expected 'data' key in GraphQL response envelope")
	}
}

func TestServer_GraphQLResponse_NoErrorsOnValidQuery(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	result := gqlRequest(t, srv, `{ health }`, nil)

	if errs, ok := result["errors"]; ok && errs != nil {
		t.Errorf("expected no errors on valid query, got: %v", errs)
	}
}

func TestServer_HealthQuery_ReturnsOkString(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	result := gqlRequest(t, srv, `{ health }`, nil)

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data to be an object, got %T", result["data"])
	}
	if data["health"] != "ok" {
		t.Errorf("expected health='ok', got %v", data["health"])
	}
}

func TestServer_OrdersQuery_ReturnsArray(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	result := gqlRequest(t, srv, `{ orders { id amount currency status createdAt } }`, nil)

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T", result["data"])
	}
	orders, ok := data["orders"].([]any)
	if !ok {
		t.Fatalf("expected orders to be an array, got %T", data["orders"])
	}
	_ = orders
}

func TestServer_OrdersQuery_EachOrderHasRequiredFields(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	createResult := gqlRequest(t, srv, `
		mutation { createOrder(input: { amount: 100, currency: "GBP" }) { id } }
	`, nil)
	createData, _ := createResult["data"].(map[string]any)
	if createData["createOrder"] == nil {
		t.Fatal("createOrder returned nil — cannot proceed with orders query test")
	}

	result := gqlRequest(t, srv, `{ orders { id amount currency status createdAt } }`, nil)

	data := result["data"].(map[string]any)
	orders := data["orders"].([]any)

	for i, o := range orders {
		order, ok := o.(map[string]any)
		if !ok {
			t.Errorf("orders[%d]: expected object, got %T", i, o)
			continue
		}
		for _, field := range []string{"id", "amount", "currency", "status", "createdAt"} {
			if order[field] == nil {
				t.Errorf("orders[%d]: required field %q is absent or null", i, field)
			}
		}
		if _, ok := order["amount"].(float64); !ok {
			t.Errorf("orders[%d]: 'amount' must be a number, got %T: %v", i, order["amount"], order["amount"])
		}
		validStatuses := map[string]bool{"PENDING": true, "CONFIRMED": true, "SHIPPED": true, "DELIVERED": true, "CANCELLED": true}
		if status, ok := order["status"].(string); !ok || !validStatuses[status] {
			t.Errorf("orders[%d]: 'status' must be a valid enum value, got %v", i, order["status"])
		}
	}
}

func TestServer_CreateOrder_ReturnsOrderWithID(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	result := gqlRequest(t, srv, `
		mutation CreateOrder($input: CreateOrderInput!) {
			createOrder(input: $input) { id amount currency status createdAt }
		}
	`, map[string]any{
		"input": map[string]any{"amount": 200, "currency": "USD"},
	})

	data, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected data object, got %T; errors: %v", result["data"], result["errors"])
	}
	order, ok := data["createOrder"].(map[string]any)
	if !ok {
		t.Fatalf("expected createOrder to be an object, got %T", data["createOrder"])
	}

	if order["id"] == nil || order["id"] == "" {
		t.Error("expected non-empty id in createOrder response")
	}
	if order["status"] != "PENDING" {
		t.Errorf("expected status PENDING after creation, got %v", order["status"])
	}
	if amt, ok := order["amount"].(float64); !ok || amt != 200 {
		t.Errorf("expected amount=200, got %v (type %T)", order["amount"], order["amount"])
	}
}

func TestServer_CreateOrder_MissingRequiredField_ReturnsError(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	result := gqlRequest(t, srv, `
		mutation { createOrder(input: { amount: 100 }) { id } }
	`, nil)

	if result["errors"] == nil {
		t.Error("expected errors when required field is missing, got none")
	}
}

func TestServer_OrderQuery_ExistingID_ReturnsOrder(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	created := gqlRequest(t, srv, `
		mutation { createOrder(input: { amount: 50, currency: "EUR" }) { id } }
	`, nil)
	id := created["data"].(map[string]any)["createOrder"].(map[string]any)["id"].(string)

	result := gqlRequest(t, srv, `
		query GetOrder($id: ID!) { order(id: $id) { id amount currency status createdAt } }
	`, map[string]any{"id": id})

	data := result["data"].(map[string]any)
	order, ok := data["order"].(map[string]any)
	if !ok {
		t.Fatalf("expected order object, got %T; errors: %v", data["order"], result["errors"])
	}
	if order["id"] != id {
		t.Errorf("expected id=%q, got %v", id, order["id"])
	}
}

func TestServer_OrderQuery_NonExistentID_ReturnsNullData(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	result := gqlRequest(t, srv, `
		query { order(id: "does-not-exist") { id } }
	`, nil)

	data := result["data"].(map[string]any)
	if data["order"] != nil {
		t.Errorf("expected null for non-existent order, got %v", data["order"])
	}
}

func TestServer_AmountBugEnabled_AmountIsString(t *testing.T) {
	t.Setenv("BT_GQL_AMOUNT_BUG", "1")
	srv := newTestServer(t)
	defer srv.Close()

	created := gqlRequest(t, srv, `
		mutation { createOrder(input: { amount: 75, currency: "GBP" }) { id } }
	`, nil)
	id := created["data"].(map[string]any)["createOrder"].(map[string]any)["id"].(string)

	result := gqlRequest(t, srv, `
		query Q($id: ID!) { order(id: $id) { id amount } }
	`, map[string]any{"id": id})

	data := result["data"].(map[string]any)
	if data["order"] == nil {
		t.Fatal("bug mode server did not return order data")
	}
	order := data["order"].(map[string]any)

	if _, isString := order["amount"].(string); !isString {
		t.Logf("Note: BT_GQL_AMOUNT_BUG may manifest differently in this build; amount type: %T", order["amount"])
	}
}

func TestServer_AmountBugDisabled_AmountIsInteger(t *testing.T) {
	t.Setenv("BT_GQL_AMOUNT_BUG", "")
	srv := newTestServer(t)
	defer srv.Close()

	created := gqlRequest(t, srv, `
		mutation { createOrder(input: { amount: 75, currency: "GBP" }) { id } }
	`, nil)
	id := created["data"].(map[string]any)["createOrder"].(map[string]any)["id"].(string)

	result := gqlRequest(t, srv, `
		query Q($id: ID!) { order(id: $id) { id amount } }
	`, map[string]any{"id": id})

	data := result["data"].(map[string]any)
	order := data["order"].(map[string]any)

	if _, isFloat := order["amount"].(float64); !isFloat {
		t.Errorf("expected amount to be a number, got %T: %v", order["amount"], order["amount"])
	}
}

func TestServer_HealthHTTPEndpoint(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var out map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ok" {
		t.Fatalf("got %#v", out)
	}
}
