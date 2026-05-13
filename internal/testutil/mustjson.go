package testutil

import (
	"encoding/json"
	"testing"
)

// MustJSON marshals v to JSON or fails the test.
func MustJSON(t testing.TB, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
