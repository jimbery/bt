package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func assertField(t *testing.T, body map[string]any, field string) {
	t.Helper()
	if _, ok := body[field]; !ok {
		t.Errorf("expected field %q in response body, got keys: %v", field, mapKeys(body))
	}
}

func assertFieldType(t *testing.T, body map[string]any, field string, wantType string) {
	t.Helper()
	val, ok := body[field]
	if !ok {
		t.Errorf("field %q absent from response body", field)
		return
	}
	gotType := jsonTypeName(val)
	if gotType != wantType {
		t.Errorf("field %q: expected type %q, got %q (value: %v)", field, wantType, gotType, val)
	}
}

func assertEnumValue(t *testing.T, body map[string]any, field string, allowed []string) {
	t.Helper()
	val, ok := body[field]
	if !ok {
		t.Errorf("field %q absent", field)
		return
	}
	s, ok := val.(string)
	if !ok {
		t.Errorf("field %q: expected string for enum check, got %T", field, val)
		return
	}
	for _, a := range allowed {
		if s == a {
			return
		}
	}
	t.Errorf("field %q: value %q not in allowed set %v", field, s, allowed)
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func jsonTypeName(v any) string {
	switch v.(type) {
	case string:
		return "string"
	case float64:
		return "number"
	case bool:
		return "boolean"
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "unknown"
	}
}

func TestHealth_SchemaShape(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	t.Run("status field is present", func(t *testing.T) {
		assertField(t, body, "status")
	})
	t.Run("status field is a string", func(t *testing.T) {
		assertFieldType(t, body, "status", "string")
	})
	t.Run("status value is 'ok'", func(t *testing.T) {
		assertEnumValue(t, body, "status", []string{"ok"})
	})
}

func TestListOrders_SchemaShape(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/orders")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	t.Run("orders field is present", func(t *testing.T) {
		assertField(t, body, "orders")
	})
	t.Run("orders field is an array", func(t *testing.T) {
		assertFieldType(t, body, "orders", "array")
	})
	t.Run("total field is present", func(t *testing.T) {
		assertField(t, body, "total")
	})
	t.Run("total field is a number", func(t *testing.T) {
		assertFieldType(t, body, "total", "number")
	})
}

func TestCreateOrder_SchemaShape(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	defer srv.Close()

	payload := `{"amount": 100, "currency": "GBP", "description": "Schema test"}`
	resp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	t.Run("id field is present and is a string", func(t *testing.T) {
		assertField(t, body, "id")
		assertFieldType(t, body, "id", "string")
	})
	t.Run("amount field is present and is a number", func(t *testing.T) {
		assertField(t, body, "amount")
		assertFieldType(t, body, "amount", "number")
	})
	t.Run("currency field is present and is a string", func(t *testing.T) {
		assertField(t, body, "currency")
		assertFieldType(t, body, "currency", "string")
	})
	t.Run("status field is present and is a valid enum value", func(t *testing.T) {
		assertField(t, body, "status")
		assertEnumValue(t, body, "status", []string{"pending", "confirmed", "shipped", "delivered", "cancelled"})
	})
	t.Run("created_at field is present and is a string", func(t *testing.T) {
		assertField(t, body, "created_at")
		assertFieldType(t, body, "created_at", "string")
	})
	t.Run("created_at field is RFC3339 parseable", func(t *testing.T) {
		createdAt, _ := body["created_at"].(string)
		if _, err := time.Parse(time.RFC3339Nano, createdAt); err != nil {
			if _, err2 := time.Parse(time.RFC3339, createdAt); err2 != nil {
				t.Errorf("created_at %q is not RFC3339: %v", createdAt, err)
			}
		}
	})
}

func TestGetOrder_SchemaShape(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	defer srv.Close()

	payload := `{"amount": 50, "currency": "USD"}`
	createResp, err := http.Post(srv.URL+"/orders", "application/json", bytes.NewBufferString(payload))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer createResp.Body.Close()
	var created map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("expected id in create response")
	}

	resp, err := http.Get(srv.URL + "/orders/" + id)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	requiredFields := []struct {
		name     string
		jsonType string
	}{
		{"id", "string"},
		{"amount", "number"},
		{"currency", "string"},
		{"status", "string"},
		{"created_at", "string"},
	}

	for _, f := range requiredFields {
		t.Run(f.name+" field present and correct type", func(t *testing.T) {
			assertField(t, body, f.name)
			assertFieldType(t, body, f.name, f.jsonType)
		})
	}

	t.Run("id matches the requested id", func(t *testing.T) {
		if body["id"] != id {
			t.Errorf("expected id %q, got %v", id, body["id"])
		}
	})
}

func TestErrorResponse_SchemaShape(t *testing.T) {
	srv := httptest.NewServer(NewRouter())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/orders", "application/json",
		bytes.NewBufferString(`{"currency":"GBP"}`))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	t.Run("error field is present and is a string", func(t *testing.T) {
		assertField(t, body, "error")
		assertFieldType(t, body, "error", "string")
	})
	t.Run("code field is present and is a string", func(t *testing.T) {
		assertField(t, body, "code")
		assertFieldType(t, body, "code", "string")
	})
}
