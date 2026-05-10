package model_test

import (
	"encoding/json"
	"testing"

	"github.com/jayimbery/bt/pkg/model"
)

func TestResponseDetail_JSONRoundTrip_embeddedJSON(t *testing.T) {
	t.Parallel()
	orig := model.ResponseDetail{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       []byte(`{"data":{"me":{"id":"1"}}}`),
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("marshaled artifact fragment invalid: %s", data)
	}
	var got model.ResponseDetail
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != string(orig.Body) {
		t.Fatalf("body mismatch: %q vs %q", got.Body, orig.Body)
	}
}

func TestResponseDetail_UnmarshalJSON_legacyBase64Body(t *testing.T) {
	t.Parallel()
	want := []byte(`{"a":1}`)
	legacy := []byte(`{"status_code":200,"body":"eyJhIjoxfQ=="}`)
	var got model.ResponseDetail
	if err := json.Unmarshal(legacy, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != string(want) {
		t.Fatalf("got %q want %q", got.Body, want)
	}
}

func TestRequestDetail_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	orig := model.RequestDetail{
		Method: "POST",
		URL:    "/graphql",
		Body:   []byte(`{"query":"{ me { id } }"}`),
	}
	data, err := json.Marshal(orig)
	if err != nil {
		t.Fatal(err)
	}
	var got model.RequestDetail
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != string(orig.Body) {
		t.Fatalf("body mismatch")
	}
}
