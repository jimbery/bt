package har_test

import (
	"strings"
	"testing"
	"time"

	"github.com/jayimbery/bt/internal/trace/har"
)

const minimalHAR = `{
  "log": {
    "version": "1.2",
    "entries": [
      {
        "startedDateTime": "2024-01-15T10:00:00Z",
        "time": 45.2,
        "request": {
          "method": "POST",
          "url": "https://api.example.com/orders",
          "headers": [],
          "postData": {
            "mimeType": "application/json",
            "text": "{\"amount\": 100, \"currency\": \"GBP\"}"
          }
        },
        "response": {
          "status": 201,
          "headers": [],
          "content": {
            "mimeType": "application/json",
            "text": "{\"id\": \"ord_1\", \"status\": \"pending\"}"
          }
        }
      }
    ]
  }
}`

const harWithNonJSONBody = `{
  "log": {
    "version": "1.2",
    "entries": [
      {
        "startedDateTime": "2024-01-15T10:00:00Z",
        "time": 10.0,
        "request": {
          "method": "GET",
          "url": "https://api.example.com/image.png",
          "headers": []
        },
        "response": {
          "status": 200,
          "headers": [],
          "content": {
            "mimeType": "image/png",
            "encoding": "base64",
            "text": "iVBORw0KGgo="
          }
        }
      }
    ]
  }
}`

const harWithMissingResponse = `{
  "log": {
    "version": "1.2",
    "entries": [
      {
        "startedDateTime": "2024-01-15T10:00:00Z",
        "time": 5000.0,
        "request": {
          "method": "GET",
          "url": "https://api.example.com/orders",
          "headers": []
        },
        "response": {
          "status": 0,
          "headers": [],
          "content": { "mimeType": "", "text": "" }
        }
      }
    ]
  }
}`

func TestHARParse_MinimalValid_ReturnsEntries(t *testing.T) {
	h, err := har.Parse(strings.NewReader(minimalHAR))
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	if len(h.Log.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(h.Log.Entries))
	}
}

func TestHARParse_Entry_MethodAndURL(t *testing.T) {
	h, _ := har.Parse(strings.NewReader(minimalHAR))
	e := h.Log.Entries[0]
	if e.Method != "POST" {
		t.Errorf("Method: want 'POST', got %q", e.Method)
	}
	if e.URL != "https://api.example.com/orders" {
		t.Errorf("URL: want 'https://api.example.com/orders', got %q", e.URL)
	}
}

func TestHARParse_Entry_RequestBodyParsedAsJSON(t *testing.T) {
	h, _ := har.Parse(strings.NewReader(minimalHAR))
	e := h.Log.Entries[0]
	if e.RequestBody == nil {
		t.Fatal("expected non-nil RequestBody for JSON request")
	}
	if string(e.RequestBody) != `{"amount": 100, "currency": "GBP"}` {
		t.Errorf("RequestBody: got %q", string(e.RequestBody))
	}
}

func TestHARParse_Entry_ResponseStatusAndBody(t *testing.T) {
	h, _ := har.Parse(strings.NewReader(minimalHAR))
	e := h.Log.Entries[0]
	if e.ResponseStatus != 201 {
		t.Errorf("ResponseStatus: want 201, got %d", e.ResponseStatus)
	}
	if e.ResponseBody == nil {
		t.Fatal("expected non-nil ResponseBody")
	}
}

func TestHARParse_Entry_StartedDateTime_ParsedAsTime(t *testing.T) {
	h, _ := har.Parse(strings.NewReader(minimalHAR))
	e := h.Log.Entries[0]
	if e.StartedDateTime.IsZero() {
		t.Error("StartedDateTime must be parsed as a non-zero time")
	}
	want := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)
	if !e.StartedDateTime.Equal(want) {
		t.Errorf("StartedDateTime: want %v, got %v", want, e.StartedDateTime)
	}
}

func TestHARParse_Entry_TimingMs(t *testing.T) {
	h, _ := har.Parse(strings.NewReader(minimalHAR))
	e := h.Log.Entries[0]
	if e.TimingMs != 45.2 {
		t.Errorf("TimingMs: want 45.2, got %f", e.TimingMs)
	}
}

func TestHARParse_NonJSONBody_BodyIsNil(t *testing.T) {
	h, err := har.Parse(strings.NewReader(harWithNonJSONBody))
	if err != nil {
		t.Fatalf("Parse: unexpected error on non-JSON body: %v", err)
	}
	e := h.Log.Entries[0]
	if e.RequestBody != nil {
		t.Error("expected nil RequestBody for non-JSON content type")
	}
	if e.ResponseBody != nil {
		t.Error("expected nil ResponseBody for non-JSON content type")
	}
}

func TestHARParse_MissingResponse_StatusZero_BodyNil(t *testing.T) {
	h, err := har.Parse(strings.NewReader(harWithMissingResponse))
	if err != nil {
		t.Fatalf("Parse: unexpected error: %v", err)
	}
	e := h.Log.Entries[0]
	if e.ResponseStatus != 0 {
		t.Errorf("expected ResponseStatus 0 for missing response, got %d", e.ResponseStatus)
	}
	if e.ResponseBody != nil {
		t.Errorf("expected nil ResponseBody for missing response")
	}
}

func TestHARParse_EmptyEntries_ReturnsNonNilHAR(t *testing.T) {
	empty := `{"log":{"version":"1.2","entries":[]}}`
	h, err := har.Parse(strings.NewReader(empty))
	if err != nil {
		t.Fatalf("Parse: unexpected error for empty entries: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil *HAR for empty entries")
	}
	if len(h.Log.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(h.Log.Entries))
	}
}

func TestHARParse_MalformedJSON_ReturnsErrHARMalformed(t *testing.T) {
	_, err := har.Parse(strings.NewReader("not json at all"))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !har.IsErrHARMalformed(err) {
		t.Errorf("expected ErrHARMalformed, got: %T: %v", err, err)
	}
}

func TestHARParse_UnsupportedVersion_ReturnsErrHARVersionUnsupported(t *testing.T) {
	bad := `{"log":{"version":"2.0","entries":[]}}`
	_, err := har.Parse(strings.NewReader(bad))
	if err == nil {
		t.Fatal("expected error for unsupported HAR version")
	}
	if !har.IsErrHARVersionUnsupported(err) {
		t.Errorf("expected ErrHARVersionUnsupported, got: %T: %v", err, err)
	}
}

func TestHARParse_Version11_Accepted(t *testing.T) {
	v11 := `{"log":{"version":"1.1","entries":[]}}`
	_, err := har.Parse(strings.NewReader(v11))
	if err != nil {
		t.Errorf("version 1.1 should be accepted, got error: %v", err)
	}
}

func TestHARFilter_MatchingHost_ReturnsMatchingEntries(t *testing.T) {
	h, _ := har.Parse(strings.NewReader(minimalHAR))
	entries := h.ToEntries()
	filtered := har.Filter(entries, "api.example.com")
	if len(filtered) != 1 {
		t.Errorf("expected 1 filtered entry, got %d", len(filtered))
	}
}

func TestHARFilter_NonMatchingHost_ReturnsEmpty(t *testing.T) {
	h, _ := har.Parse(strings.NewReader(minimalHAR))
	entries := h.ToEntries()
	filtered := har.Filter(entries, "other.example.com")
	if len(filtered) != 0 {
		t.Errorf("expected 0 filtered entries for non-matching host, got %d", len(filtered))
	}
}

func TestHARFilter_EmptyHost_ReturnsAllEntries(t *testing.T) {
	h, _ := har.Parse(strings.NewReader(minimalHAR))
	entries := h.ToEntries()
	filtered := har.Filter(entries, "")
	if len(filtered) != len(entries) {
		t.Errorf("empty host should return all entries: want %d, got %d", len(entries), len(filtered))
	}
}

func TestHARFilter_PortIgnoredInMatch(t *testing.T) {
	withPort := `{"log":{"version":"1.2","entries":[{
		"startedDateTime":"2024-01-15T10:00:00Z","time":10,
		"request":{"method":"GET","url":"https://api.example.com:8443/orders","headers":[]},
		"response":{"status":200,"headers":[],"content":{"mimeType":"application/json","text":"{}"}}
	}]}}`
	h, _ := har.Parse(strings.NewReader(withPort))
	entries := h.ToEntries()
	filtered := har.Filter(entries, "api.example.com")
	if len(filtered) != 1 {
		t.Errorf("port should be ignored in host matching; got %d entries", len(filtered))
	}
}
