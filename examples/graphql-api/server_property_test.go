package main_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	gqlapi "github.com/jayimbery/bt/examples/graphql-api"
)

func gqlPost(t *testing.T, srv *httptest.Server, query string, variables map[string]any) map[string]any {
	t.Helper()
	payload := map[string]any{"query": query}
	if variables != nil {
		payload["variables"] = variables
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(srv.URL+"/graphql", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gqlPost: %v", err)
	}
	defer resp.Body.Close()
	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return result
}

func assertNoErrors(t *testing.T, response map[string]any) {
	t.Helper()
	errorsRaw, ok := response["errors"]
	if !ok || errorsRaw == nil {
		return
	}
	arr, ok := errorsRaw.([]any)
	if !ok || len(arr) == 0 {
		return
	}
	t.Errorf("unexpected GraphQL errors: %v", errorsRaw)
}

func assertDataField(t *testing.T, response map[string]any, key string) map[string]any {
	t.Helper()
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("response.data is not an object: %T", response["data"])
	}
	val, ok := data[key]
	if !ok {
		t.Fatalf("response.data.%s is absent; data keys: %v", key, mapKeysAny(data))
	}
	result, ok := val.(map[string]any)
	if !ok {
		t.Fatalf("response.data.%s is not an object: %T", key, val)
	}
	return result
}

func mapKeysAny(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func TestCreateOrder_PropertyShape_ValidInput_NoErrors(t *testing.T) {
	srv := httptest.NewServer(gqlapi.NewHandler())
	defer srv.Close()

	response := gqlPost(t, srv, `
		mutation CreateOrder($input: CreateOrderInput!) {
			createOrder(input: $input) { id amount currency status createdAt }
		}
	`, map[string]any{
		"input": map[string]any{"amount": 100, "currency": "GBP"},
	})

	t.Run("no errors in envelope", func(t *testing.T) {
		assertNoErrors(t, response)
	})

	order := assertDataField(t, response, "createOrder")

	t.Run("id is a non-empty string", func(t *testing.T) {
		id, ok := order["id"].(string)
		if !ok || id == "" {
			t.Errorf("expected non-empty string for 'id', got: %v (%T)", order["id"], order["id"])
		}
	})

	t.Run("amount is an integer", func(t *testing.T) {
		amountRaw := order["amount"]
		switch v := amountRaw.(type) {
		case float64:
			if v != float64(int64(v)) {
				t.Errorf("amount %v is not an integer-valued number", v)
			}
		default:
			t.Errorf("amount expected to be numeric, got %T: %v", amountRaw, amountRaw)
		}
	})

	t.Run("currency is a string", func(t *testing.T) {
		if _, ok := order["currency"].(string); !ok {
			t.Errorf("currency expected string, got %T: %v", order["currency"], order["currency"])
		}
	})

	t.Run("status is a valid OrderStatus enum value", func(t *testing.T) {
		validStatuses := map[string]bool{"PENDING": true, "CONFIRMED": true, "SHIPPED": true, "DELIVERED": true, "CANCELLED": true}
		status, ok := order["status"].(string)
		if !ok {
			t.Errorf("status expected string, got %T", order["status"])
			return
		}
		if !validStatuses[status] {
			t.Errorf("status %q is not a valid OrderStatus", status)
		}
	})

	t.Run("createdAt is a non-empty string", func(t *testing.T) {
		if s, ok := order["createdAt"].(string); !ok || s == "" {
			t.Errorf("createdAt expected non-empty string, got %v (%T)", order["createdAt"], order["createdAt"])
		}
	})
}

func TestCreateOrder_PropertyShape_ZeroAmount_HandledGracefully(t *testing.T) {
	srv := httptest.NewServer(gqlapi.NewHandler())
	defer srv.Close()

	response := gqlPost(t, srv, `
		mutation CreateOrder($input: CreateOrderInput!) {
			createOrder(input: $input) { id amount status }
		}
	`, map[string]any{
		"input": map[string]any{"amount": 0, "currency": "USD"},
	})

	t.Run("response has either data or errors — never both absent", func(t *testing.T) {
		hasData := response["data"] != nil
		hasErrors := response["errors"] != nil
		if !hasData && !hasErrors {
			t.Error("response must have either 'data' or 'errors' key")
		}
	})
}

func TestCreateOrder_PropertyShape_NegativeAmount_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(gqlapi.NewHandler())
	defer srv.Close()

	response := gqlPost(t, srv, `
		mutation CreateOrder($input: CreateOrderInput!) {
			createOrder(input: $input) { id amount status }
		}
	`, map[string]any{
		"input": map[string]any{"amount": -1, "currency": "GBP"},
	})

	t.Run("negative amount produces error or null data", func(t *testing.T) {
		hasErrors := func() bool {
			arr, ok := response["errors"].([]any)
			return ok && len(arr) > 0
		}()
		dataIsNull := response["data"] == nil
		if !hasErrors && !dataIsNull {
			t.Errorf("expected error or null data for negative amount; got: %v", response)
		}
	})
}

func TestOrderQuery_PropertyShape_ExistingOrder(t *testing.T) {
	srv := httptest.NewServer(gqlapi.NewHandler())
	defer srv.Close()

	createResp := gqlPost(t, srv, `
		mutation { createOrder(input: {amount: 50, currency: "EUR"}) { id } }
	`, nil)
	assertNoErrors(t, createResp)
	orderID := assertDataField(t, createResp, "createOrder")["id"].(string)

	response := gqlPost(t, srv, `
		query GetOrder($id: ID!) { order(id: $id) { id amount currency status } }
	`, map[string]any{"id": orderID})

	t.Run("no errors in envelope", func(t *testing.T) {
		assertNoErrors(t, response)
	})

	order := assertDataField(t, response, "order")

	t.Run("id matches requested id", func(t *testing.T) {
		if order["id"] != orderID {
			t.Errorf("expected id %q, got %v", orderID, order["id"])
		}
	})

	t.Run("amount is numeric", func(t *testing.T) {
		if _, ok := order["amount"].(float64); !ok {
			t.Errorf("amount expected float64, got %T: %v", order["amount"], order["amount"])
		}
	})
}

func TestOrderQuery_PropertyShape_NonExistentID_NullData(t *testing.T) {
	srv := httptest.NewServer(gqlapi.NewHandler())
	defer srv.Close()

	response := gqlPost(t, srv, `
		query GetOrder($id: ID!) { order(id: $id) { id amount status } }
	`, map[string]any{"id": "does-not-exist"})

	assertNoErrors(t, response)
	data, ok := response["data"].(map[string]any)
	if !ok {
		t.Fatalf("response.data is not an object")
	}
	if order, ok := data["order"]; ok && order != nil {
		t.Errorf("expected null order for non-existent id, got %v", order)
	}
}

func TestOrderQuery_BrokenResolver_AmountIsString(t *testing.T) {
	if os.Getenv("BT_GQL_AMOUNT_BUG") != "1" {
		t.Skip("BT_GQL_AMOUNT_BUG not set — skipping broken resolver test")
	}

	srv := httptest.NewServer(gqlapi.NewHandler())
	defer srv.Close()

	createResp := gqlPost(t, srv, `mutation { createOrder(input: {amount: 100, currency: "GBP"}) { id } }`, nil)
	id := assertDataField(t, createResp, "createOrder")["id"].(string)

	response := gqlPost(t, srv, `query Q($id: ID!) { order(id: $id) { id amount status } }`,
		map[string]any{"id": id})

	order := assertDataField(t, response, "order")
	amountRaw := order["amount"]

	t.Run("broken resolver returns amount as string (this is the bug)", func(t *testing.T) {
		if _, ok := amountRaw.(string); !ok {
			t.Errorf("expected broken resolver to return amount as string, got %T: %v", amountRaw, amountRaw)
		}
	})
}
