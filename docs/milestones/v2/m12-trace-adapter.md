# M12 — Trace Adapter (HAR Import)

This document follows the project convention: spec first, tests second, implementation third. No implementation file is written until the tests for it exist and clearly fail. The tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

**TDD order for this milestone:**
1. Write and verify all tests in this document (they will fail — no implementation exists yet)
2. Write implementation until all tests pass
3. Proceed to M12.5 integration tests

**ADR references:**
- ADR-008 — TraceProfile storage format and location
- ADR-009 — Generator composition (trace distributions + schema generators)
- ADR-010 — Sequence representation (Markov chain in TraceProfile)

---

## Overview

M12 is a two-stage pipeline. Stage 1 parses a HAR file and extracts statistical patterns. Stage 2 writes those patterns to a `TraceProfile` that the property strategy consumes instead of its default uniform generators.

The five pieces built here:

1. **HAR parser** (`internal/trace/har`) — parses HAR 1.2 JSON, extracts entries matching a service host filter, maps entries to normalised `HAREntry` values
2. **Pattern extractor** (`internal/trace/analyze`) — produces frequency distributions, value distributions, null rates, always-present flags, and Markov sequence data from a slice of `HAREntry`
3. **`TraceProfile` model** (`pkg/model/trace.go`) — the JSON-serialisable output of analysis; parseable with strict error handling per ADR-008
4. **`ComposedGenerator`** (`internal/strategy/property/gen/composed.go`) — wraps schema-derived generators with trace-derived distributions per ADR-009
5. **CLI commands** — `bt trace import <har-file>` and `bt trace inspect`

**Explicitly out of scope for M12:** live traffic capture, PII detection, Datadog integration, sequence-based test generation (sequences are extracted but consumed in M13).

**Exit criterion:** `bt trace import <har-file>` writes a valid `TraceProfile`. `bt run --strategy property` with a profile configured generates `currency` values matching the HAR-observed distribution within tolerance over 1000 draws. All unit tests pass with `-race`.

---

## Step 1 — HAR parser

### Spec

- Package: `internal/trace/har`
- `Parse(r io.Reader) (*HAR, error)` — parses HAR 1.2 JSON from a reader
- `HAR` contains a `Log` with `[]Entry`; `Entry` maps to `HAREntry` after normalisation
- `HAREntry` carries: `URL`, `Method`, `RequestBody` (`[]byte`, nil if absent or non-JSON), `ResponseStatus`, `ResponseBody` (`[]byte`, nil if absent or non-JSON), `StartedDateTime` (parsed `time.Time`), `TimingMs` (total elapsed)
- `Filter(entries []HAREntry, host string) []HAREntry` — returns only entries whose URL host matches the provided host string (exact match on hostname, port ignored)
- Entries with no response (e.g. network errors in the HAR) are included with `ResponseStatus: 0` and `ResponseBody: nil` — they are not silently dropped
- Non-JSON request or response bodies are included with the body set to nil — the parser does not error on binary content types
- A HAR file with no entries is valid — `Parse` returns a non-nil `*HAR` with an empty `Log.Entries`
- Malformed JSON at the top level returns `ErrHARMalformed`
- A HAR file where `log.version` is not `"1.1"` or `"1.2"` returns `ErrHARVersionUnsupported`

### Tests

`internal/trace/har/har_test.go`:

```go
package har_test

import (
	"strings"
	"testing"
	"time"

	"github.com/yourorg/bt/internal/trace/har"
)

// --- fixtures ---

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

// --- Parse ---

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
	// Must be the raw JSON bytes
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

// --- Filter ---

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
```

Run and confirm tests fail:

```bash
go test ./internal/trace/har/... -race -v
```

---

## Step 2 — Pattern extractor

### Spec

- Package: `internal/trace/analyze`
- `Analyze(entries []har.HAREntry, spec *openapi3.T) (*model.TraceProfile, error)` — the main entry point
- The spec is used to match URL path patterns to `operationId`s (e.g. `POST /orders` → `CreateOrder`); entries that do not match any operation are silently skipped with a `DEBUG` log
- For each matched operation, the extractor produces:
  - `CallCount` — number of matched entries
  - `FrequencyRank` — rank by call count (1 = most frequent)
  - `Arguments` — per-argument `ArgumentProfile`:
    - `Type` — inferred from observed values (`string`, `integer`, `number`, `boolean`)
    - `Samples` — up to 1000 raw observed values (capped to prevent unbounded memory)
    - `Distribution` — normalised frequency map; only populated when `len(Samples) >= 20`; values sum to 1.0 ± 0.001
    - `Range` — `{Min, Max}` for numeric arguments; nil for non-numeric
    - `NullRate` — fraction of observations where the value was null (0.0–1.0)
    - `AlwaysPresent` — true when the field appeared in every request
- Session identification: entries within a 30-minute window with the same source IP (or session cookie if present) are grouped into a session for Markov chain extraction
- `Sequences` populated per ADR-010: `StartProbability`, `Transitions` (including `__END__`), `MinObservedSessionLength`, `MaxObservedSessionLength`
- Every transition row must sum to 1.0 ± 0.001; `Analyze` returns `ErrSequenceNormalization` if any row cannot be normalised (e.g. division by zero)
- `GeneratedAt` is set to `time.Now().UTC().Format(time.RFC3339)` at analysis time
- `SchemaVersion` is always `"1"`
- `SourceHAR` is set to the base filename of the HAR file (not the full path, for portability)

### Tests

`internal/trace/analyze/analyze_test.go`:

```go
package analyze_test

import (
	"testing"
	"time"

	"github.com/yourorg/bt/internal/trace/analyze"
	"github.com/yourorg/bt/internal/trace/har"
	"github.com/yourorg/bt/pkg/model"
)

// buildEntries constructs a slice of HAREntry values for testing.
func buildEntries(t *testing.T, ops []struct {
	method, path, reqBody, respBody string
	status                          int
	startedAt                       time.Time
}) []har.HAREntry {
	t.Helper()
	entries := make([]har.HAREntry, len(ops))
	for i, op := range ops {
		entries[i] = har.HAREntry{
			Method:         op.method,
			URL:            "https://api.example.com" + op.path,
			RequestBody:    jsonBytes(op.reqBody),
			ResponseBody:   jsonBytes(op.respBody),
			ResponseStatus: op.status,
			StartedDateTime: op.startedAt,
		}
	}
	return entries
}

func jsonBytes(s string) []byte {
	if s == "" {
		return nil
	}
	return []byte(s)
}

func minimalSpec(t *testing.T) *openapi3.T {
	t.Helper()
	// Returns a minimal OpenAPI spec with CreateOrder (POST /orders)
	// and GetOrder (GET /orders/{id}) operations
	return mustParseSpec(t, `
openapi: "3.0.3"
info: {title: Test, version: "1.0"}
paths:
  /orders:
    post:
      operationId: CreateOrder
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                amount:   {type: integer}
                currency: {type: string}
  /orders/{id}:
    get:
      operationId: GetOrder
      parameters:
        - {name: id, in: path, schema: {type: string}}
`)
}

// --- CallCount and FrequencyRank ---

func TestAnalyze_CallCount_MatchedOperations(t *testing.T) {
	now := time.Now()
	entries := buildEntries(t, []struct {
		method, path, reqBody, respBody string
		status                          int
		startedAt                       time.Time
	}{
		{"POST", "/orders", `{"amount":100,"currency":"GBP"}`, `{"id":"o1"}`, 201, now},
		{"POST", "/orders", `{"amount":200,"currency":"USD"}`, `{"id":"o2"}`, 201, now.Add(time.Second)},
		{"GET", "/orders/o1", "", `{"id":"o1","amount":100}`, 200, now.Add(2 * time.Second)},
	})

	profile, err := analyze.Analyze(entries, minimalSpec(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	t.Run("CreateOrder has call count 2", func(t *testing.T) {
		op, ok := profile.Operations["CreateOrder"]
		if !ok {
			t.Fatal("expected CreateOrder in profile")
		}
		if op.CallCount != 2 {
			t.Errorf("CallCount: want 2, got %d", op.CallCount)
		}
	})

	t.Run("GetOrder has call count 1", func(t *testing.T) {
		op, ok := profile.Operations["GetOrder"]
		if !ok {
			t.Fatal("expected GetOrder in profile")
		}
		if op.CallCount != 1 {
			t.Errorf("CallCount: want 1, got %d", op.CallCount)
		}
	})

	t.Run("CreateOrder has frequency rank 1 (most frequent)", func(t *testing.T) {
		if profile.Operations["CreateOrder"].FrequencyRank != 1 {
			t.Errorf("FrequencyRank: want 1, got %d", profile.Operations["CreateOrder"].FrequencyRank)
		}
	})
}

func TestAnalyze_UnmatchedEntries_Skipped(t *testing.T) {
	now := time.Now()
	entries := buildEntries(t, []struct {
		method, path, reqBody, respBody string
		status                          int
		startedAt                       time.Time
	}{
		{"DELETE", "/unknown/path", "", "", 404, now}, // no matching operation
		{"POST", "/orders", `{"amount":50,"currency":"EUR"}`, `{"id":"o1"}`, 201, now.Add(time.Second)},
	})

	profile, err := analyze.Analyze(entries, minimalSpec(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if _, ok := profile.Operations["UnknownOp"]; ok {
		t.Error("unmatched entry should not produce an operation in the profile")
	}
	if profile.Operations["CreateOrder"].CallCount != 1 {
		t.Errorf("matched entry should have call count 1, got %d", profile.Operations["CreateOrder"].CallCount)
	}
}

// --- ArgumentProfile: Distribution ---

func TestAnalyze_Distribution_SufficientSamples_Populated(t *testing.T) {
	// Generate 30 POST /orders entries with currency distributed GBP:21, USD:6, EUR:3
	now := time.Now()
	entries := make([]har.HAREntry, 0, 30)
	currencies := make([]string, 0, 30)
	for i := 0; i < 21; i++ {
		currencies = append(currencies, "GBP")
	}
	for i := 0; i < 6; i++ {
		currencies = append(currencies, "USD")
	}
	for i := 0; i < 3; i++ {
		currencies = append(currencies, "EUR")
	}
	for i, c := range currencies {
		entries = append(entries, har.HAREntry{
			Method:          "POST",
			URL:             "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":100,"currency":"` + c + `"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		})
	}

	profile, err := analyze.Analyze(entries, minimalSpec(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg, ok := profile.Operations["CreateOrder"].Arguments["currency"]
	if !ok {
		t.Fatal("expected 'currency' argument in profile")
	}

	t.Run("distribution is populated with sufficient samples", func(t *testing.T) {
		if len(arg.Distribution) == 0 {
			t.Error("expected non-empty distribution for 30 samples")
		}
	})

	t.Run("GBP has highest probability (~0.70)", func(t *testing.T) {
		if arg.Distribution["GBP"] < 0.60 || arg.Distribution["GBP"] > 0.80 {
			t.Errorf("GBP distribution: want ~0.70, got %f", arg.Distribution["GBP"])
		}
	})

	t.Run("distribution sums to 1.0", func(t *testing.T) {
		total := 0.0
		for _, p := range arg.Distribution {
			total += p
		}
		if total < 0.999 || total > 1.001 {
			t.Errorf("distribution does not sum to 1.0: got %f", total)
		}
	})
}

func TestAnalyze_Distribution_InsufficientSamples_NotPopulated(t *testing.T) {
	// Only 5 entries — below the 20-sample threshold
	now := time.Now()
	entries := make([]har.HAREntry, 5)
	for i := range entries {
		entries[i] = har.HAREntry{
			Method:          "POST",
			URL:             "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":100,"currency":"GBP"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, err := analyze.Analyze(entries, minimalSpec(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg := profile.Operations["CreateOrder"].Arguments["currency"]
	if len(arg.Distribution) != 0 {
		t.Errorf("expected empty distribution for fewer than 20 samples, got: %v", arg.Distribution)
	}
	if len(arg.Samples) != 5 {
		t.Errorf("expected 5 samples, got %d", len(arg.Samples))
	}
}

// --- ArgumentProfile: Range ---

func TestAnalyze_Range_NumericArgument_Populated(t *testing.T) {
	now := time.Now()
	amounts := []int{10, 50, 100, 200, 500, 25, 75, 150, 300, 400,
		12, 60, 110, 210, 510, 35, 85, 160, 310, 410,
		15, 55, 105}
	entries := make([]har.HAREntry, len(amounts))
	for i, a := range amounts {
		entries[i] = har.HAREntry{
			Method:          "POST",
			URL:             "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":` + itoa(a) + `,"currency":"GBP"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, err := analyze.Analyze(entries, minimalSpec(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg := profile.Operations["CreateOrder"].Arguments["amount"]

	t.Run("range is non-nil for numeric argument", func(t *testing.T) {
		if arg.Range == nil {
			t.Fatal("expected non-nil Range for integer argument 'amount'")
		}
	})
	t.Run("range min is correct", func(t *testing.T) {
		if arg.Range.Min != 10 {
			t.Errorf("Range.Min: want 10, got %f", arg.Range.Min)
		}
	})
	t.Run("range max is correct", func(t *testing.T) {
		if arg.Range.Max != 510 {
			t.Errorf("Range.Max: want 510, got %f", arg.Range.Max)
		}
	})
}

func TestAnalyze_Range_StringArgument_Nil(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 25)
	for i := range entries {
		entries[i] = har.HAREntry{
			Method:          "POST",
			URL:             "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":100,"currency":"GBP"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, _ := analyze.Analyze(entries, minimalSpec(t))
	arg := profile.Operations["CreateOrder"].Arguments["currency"]
	if arg.Range != nil {
		t.Errorf("expected nil Range for string argument 'currency', got: %+v", arg.Range)
	}
}

// --- ArgumentProfile: NullRate and AlwaysPresent ---

func TestAnalyze_NullRate_CalculatedFromObservations(t *testing.T) {
	// 20 entries: 10 with description present, 10 with description absent (null)
	// NullRate for description should be ~0.50
	now := time.Now()
	entries := make([]har.HAREntry, 20)
	for i := range entries {
		if i%2 == 0 {
			entries[i] = har.HAREntry{
				Method: "POST", URL: "https://api.example.com/orders",
				RequestBody:     []byte(`{"amount":100,"currency":"GBP","description":"test"}`),
				ResponseStatus:  201,
				StartedDateTime: now.Add(time.Duration(i) * time.Second),
			}
		} else {
			entries[i] = har.HAREntry{
				Method: "POST", URL: "https://api.example.com/orders",
				RequestBody:     []byte(`{"amount":100,"currency":"GBP"}`),
				ResponseStatus:  201,
				StartedDateTime: now.Add(time.Duration(i) * time.Second),
			}
		}
	}

	profile, err := analyze.Analyze(entries, minimalSpec(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	arg, ok := profile.Operations["CreateOrder"].Arguments["description"]
	if !ok {
		t.Skip("description argument not extracted — schema may not include it")
	}

	t.Run("null rate is approximately 0.50", func(t *testing.T) {
		if arg.NullRate < 0.40 || arg.NullRate > 0.60 {
			t.Errorf("NullRate: want ~0.50, got %f", arg.NullRate)
		}
	})
}

func TestAnalyze_AlwaysPresent_WhenFieldAppearsInAllEntries(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 25)
	for i := range entries {
		entries[i] = har.HAREntry{
			Method: "POST", URL: "https://api.example.com/orders",
			RequestBody:     []byte(`{"amount":100,"currency":"GBP"}`),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, _ := analyze.Analyze(entries, minimalSpec(t))
	currencyArg := profile.Operations["CreateOrder"].Arguments["currency"]

	if !currencyArg.AlwaysPresent {
		t.Error("expected AlwaysPresent=true for 'currency' which appears in every request")
	}
}

func TestAnalyze_AlwaysPresent_FalseWhenFieldSometimesAbsent(t *testing.T) {
	now := time.Now()
	entries := make([]har.HAREntry, 25)
	for i := range entries {
		body := `{"amount":100,"currency":"GBP"}`
		if i%5 == 0 {
			body = `{"amount":100}` // currency absent
		}
		entries[i] = har.HAREntry{
			Method: "POST", URL: "https://api.example.com/orders",
			RequestBody:     []byte(body),
			ResponseStatus:  201,
			StartedDateTime: now.Add(time.Duration(i) * time.Second),
		}
	}

	profile, _ := analyze.Analyze(entries, minimalSpec(t))
	currencyArg := profile.Operations["CreateOrder"].Arguments["currency"]

	if currencyArg.AlwaysPresent {
		t.Error("expected AlwaysPresent=false for 'currency' which is sometimes absent")
	}
}

// --- TraceProfile metadata ---

func TestAnalyze_SchemaVersion_IsOne(t *testing.T) {
	profile, _ := analyze.Analyze(nil, minimalSpec(t))
	if profile.SchemaVersion != "1" {
		t.Errorf("SchemaVersion: want '1', got %q", profile.SchemaVersion)
	}
}

func TestAnalyze_GeneratedAt_IsRFC3339(t *testing.T) {
	profile, _ := analyze.Analyze(nil, minimalSpec(t))
	if _, err := time.Parse(time.RFC3339, profile.GeneratedAt); err != nil {
		t.Errorf("GeneratedAt %q is not RFC3339: %v", profile.GeneratedAt, err)
	}
}

// --- Sequences (Markov chain) ---

func TestAnalyze_Sequences_StartProbability_SumsToOne(t *testing.T) {
	now := time.Now()
	// Simulate 2 sessions: [CreateOrder, GetOrder] and [CreateOrder, GetOrder]
	entries := []har.HAREntry{
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":100,"currency":"GBP"}`), ResponseStatus: 201, StartedDateTime: now},
		{Method: "GET", URL: "https://api.example.com/orders/o1", ResponseStatus: 200, StartedDateTime: now.Add(5 * time.Second)},
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":50,"currency":"USD"}`), ResponseStatus: 201, StartedDateTime: now.Add(2000 * time.Second)}, // new session
		{Method: "GET", URL: "https://api.example.com/orders/o2", ResponseStatus: 200, StartedDateTime: now.Add(2005 * time.Second)},
	}

	profile, err := analyze.Analyze(entries, minimalSpec(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if profile.Sequences == nil {
		t.Fatal("expected non-nil Sequences")
	}

	total := 0.0
	for _, p := range profile.Sequences.StartProbability {
		total += p
	}
	if total < 0.999 || total > 1.001 {
		t.Errorf("StartProbability does not sum to 1.0: got %f", total)
	}
}

func TestAnalyze_Sequences_TransitionRows_SumToOne(t *testing.T) {
	now := time.Now()
	entries := []har.HAREntry{
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":100,"currency":"GBP"}`), ResponseStatus: 201, StartedDateTime: now},
		{Method: "GET", URL: "https://api.example.com/orders/o1", ResponseStatus: 200, StartedDateTime: now.Add(5 * time.Second)},
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":50,"currency":"USD"}`), ResponseStatus: 201, StartedDateTime: now.Add(2000 * time.Second)},
		{Method: "GET", URL: "https://api.example.com/orders/o2", ResponseStatus: 200, StartedDateTime: now.Add(2005 * time.Second)},
	}

	profile, err := analyze.Analyze(entries, minimalSpec(t))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	for opID, row := range profile.Sequences.Transitions {
		total := 0.0
		for _, p := range row {
			total += p
		}
		if total < 0.999 || total > 1.001 {
			t.Errorf("transition row for %q does not sum to 1.0: got %f", opID, total)
		}
	}
}

func TestAnalyze_Sequences_CreateOrder_HasENDSentinel(t *testing.T) {
	now := time.Now()
	// Single-entry session: CreateOrder followed by nothing → END probability 1.0
	entries := []har.HAREntry{
		{Method: "POST", URL: "https://api.example.com/orders", RequestBody: []byte(`{"amount":100,"currency":"GBP"}`), ResponseStatus: 201, StartedDateTime: now},
	}

	profile, _ := analyze.Analyze(entries, minimalSpec(t))

	if profile.Sequences == nil {
		t.Fatal("expected Sequences to be non-nil")
	}
	row, ok := profile.Sequences.Transitions["CreateOrder"]
	if !ok {
		t.Fatal("expected CreateOrder in Transitions")
	}
	if _, hasEnd := row["__END__"]; !hasEnd {
		t.Error("expected __END__ sentinel in CreateOrder transition row")
	}
}

// --- helper ---
func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
```

Run and confirm tests fail:

```bash
go test ./internal/trace/analyze/... -race -v
```

---

## Step 3 — `TraceProfile` model and persistence

### Spec

- Package: `pkg/model/trace.go`
- `ParseProfile(path string) (*TraceProfile, error)` — reads and parses `TraceProfile` JSON from disk
- Error types per ADR-008: `ErrProfileNotFound`, `ErrProfileMalformed`, `ErrProfileVersionMismatch`
- `TraceProfile.WriteToFile(path string) error` — serialises to JSON and writes to path; creates parent directories if absent
- `ArgumentProfile.Distribution` normalisation is validated on parse: any row that sums outside 1.0 ± 0.001 returns `ErrProfileMalformed`
- The model package must not import `internal/trace` — the dependency flows one way

### Tests

`pkg/model/trace_test.go`:

```go
package model_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yourorg/bt/pkg/model"
)

func writeTempProfile(t *testing.T, content any) string {
	t.Helper()
	data, err := json.Marshal(content)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func validProfileData() map[string]any {
	return map[string]any{
		"schema_version": "1",
		"generated_at":   "2024-01-15T10:00:00Z",
		"source_har":     "sample.har",
		"operations": map[string]any{
			"CreateOrder": map[string]any{
				"call_count":     30,
				"frequency_rank": 1,
				"arguments": map[string]any{
					"currency": map[string]any{
						"type":           "string",
						"samples":        []string{"GBP", "USD", "EUR"},
						"distribution":   map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05},
						"null_rate":      0.0,
						"always_present": true,
					},
				},
			},
		},
		"sequences": map[string]any{
			"start_probability":             map[string]float64{"CreateOrder": 1.0},
			"transitions":                   map[string]any{"CreateOrder": map[string]float64{"__END__": 1.0}},
			"min_observed_session_length":   1,
			"max_observed_session_length":   1,
		},
	}
}

func TestParseProfile_ValidFile_ParsesCorrectly(t *testing.T) {
	path := writeTempProfile(t, validProfileData())
	profile, err := model.ParseProfile(path)
	if err != nil {
		t.Fatalf("ParseProfile: %v", err)
	}

	t.Run("schema version is '1'", func(t *testing.T) {
		if profile.SchemaVersion != "1" {
			t.Errorf("want '1', got %q", profile.SchemaVersion)
		}
	})
	t.Run("generated_at is RFC3339 parseable", func(t *testing.T) {
		if _, err := time.Parse(time.RFC3339, profile.GeneratedAt); err != nil {
			t.Errorf("GeneratedAt not RFC3339: %v", err)
		}
	})
	t.Run("CreateOrder operation is present", func(t *testing.T) {
		if _, ok := profile.Operations["CreateOrder"]; !ok {
			t.Error("expected CreateOrder in Operations")
		}
	})
	t.Run("currency argument distribution is populated", func(t *testing.T) {
		arg := profile.Operations["CreateOrder"].Arguments["currency"]
		if len(arg.Distribution) == 0 {
			t.Error("expected non-empty distribution for currency")
		}
		if arg.Distribution["GBP"] != 0.70 {
			t.Errorf("GBP distribution: want 0.70, got %f", arg.Distribution["GBP"])
		}
	})
	t.Run("always_present is true for currency", func(t *testing.T) {
		arg := profile.Operations["CreateOrder"].Arguments["currency"]
		if !arg.AlwaysPresent {
			t.Error("expected AlwaysPresent=true for currency")
		}
	})
	t.Run("sequences are present", func(t *testing.T) {
		if profile.Sequences == nil {
			t.Error("expected non-nil Sequences")
		}
		if profile.Sequences.StartProbability["CreateOrder"] != 1.0 {
			t.Errorf("StartProbability[CreateOrder]: want 1.0, got %f",
				profile.Sequences.StartProbability["CreateOrder"])
		}
	})
}

func TestParseProfile_MissingFile_ReturnsErrProfileNotFound(t *testing.T) {
	_, err := model.ParseProfile("/nonexistent/path/profile.json")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !model.IsErrProfileNotFound(err) {
		t.Errorf("expected ErrProfileNotFound, got: %T: %v", err, err)
	}
}

func TestParseProfile_InvalidJSON_ReturnsErrProfileMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "profile.json")
	os.WriteFile(path, []byte("not json"), 0644)

	_, err := model.ParseProfile(path)
	if !model.IsErrProfileMalformed(err) {
		t.Errorf("expected ErrProfileMalformed, got: %T: %v", err, err)
	}
}

func TestParseProfile_UnknownSchemaVersion_ReturnsErrProfileVersionMismatch(t *testing.T) {
	data := validProfileData()
	data["schema_version"] = "99"
	path := writeTempProfile(t, data)

	_, err := model.ParseProfile(path)
	if !model.IsErrProfileVersionMismatch(err) {
		t.Errorf("expected ErrProfileVersionMismatch, got: %T: %v", err, err)
	}
	if err != nil && !containsString(err.Error(), "upgrade") {
		t.Errorf("error message should mention 'upgrade', got: %v", err)
	}
}

func TestParseProfile_MissingOperationsKey_ReturnsErrProfileMalformed(t *testing.T) {
	data := map[string]any{
		"schema_version": "1",
		"generated_at":   "2024-01-15T10:00:00Z",
	}
	path := writeTempProfile(t, data)
	_, err := model.ParseProfile(path)
	if !model.IsErrProfileMalformed(err) {
		t.Errorf("expected ErrProfileMalformed for missing 'operations', got: %T: %v", err, err)
	}
}

func TestParseProfile_DistributionDoesNotSumToOne_ReturnsErrProfileMalformed(t *testing.T) {
	data := validProfileData()
	// Corrupt the distribution so it sums to 1.5
	data["operations"].(map[string]any)["CreateOrder"].(map[string]any)["arguments"].(map[string]any)["currency"].(map[string]any)["distribution"] = map[string]float64{
		"GBP": 1.0,
		"USD": 0.50, // intentionally broken
	}
	path := writeTempProfile(t, data)
	_, err := model.ParseProfile(path)
	if !model.IsErrProfileMalformed(err) {
		t.Errorf("expected ErrProfileMalformed for non-normalised distribution, got: %T: %v", err, err)
	}
}

func TestTraceProfile_WriteToFile_RoundTrips(t *testing.T) {
	original := &model.TraceProfile{
		SchemaVersion: "1",
		GeneratedAt:   "2024-01-15T10:00:00Z",
		SourceHAR:     "sample.har",
		Operations: map[string]*model.OperationProfile{
			"CreateOrder": {
				CallCount:     30,
				FrequencyRank: 1,
				Arguments: map[string]*model.ArgumentProfile{
					"currency": {
						Type:         "string",
						Samples:      []any{"GBP", "USD"},
						Distribution: map[string]float64{"GBP": 0.70, "USD": 0.30},
						AlwaysPresent: true,
					},
				},
			},
		},
		Sequences: &model.SequenceProfile{
			StartProbability: map[string]float64{"CreateOrder": 1.0},
			Transitions:      map[string]map[string]float64{"CreateOrder": {"__END__": 1.0}},
			MinObservedSessionLength: 1,
			MaxObservedSessionLength: 1,
		},
	}

	dir := t.TempDir()
	path := filepath.Join(dir, ".bt", "trace", "profile.json")

	if err := original.WriteToFile(path); err != nil {
		t.Fatalf("WriteToFile: %v", err)
	}

	t.Run("file exists", func(t *testing.T) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Error("expected file to exist after WriteToFile")
		}
	})

	t.Run("parent directories are created", func(t *testing.T) {
		if _, err := os.Stat(filepath.Dir(path)); os.IsNotExist(err) {
			t.Error("expected parent directories to be created by WriteToFile")
		}
	})

	parsed, err := model.ParseProfile(path)
	if err != nil {
		t.Fatalf("ParseProfile after WriteToFile: %v", err)
	}

	t.Run("schema version survives round-trip", func(t *testing.T) {
		if parsed.SchemaVersion != original.SchemaVersion {
			t.Errorf("want %q, got %q", original.SchemaVersion, parsed.SchemaVersion)
		}
	})
	t.Run("operation count survives round-trip", func(t *testing.T) {
		if len(parsed.Operations) != len(original.Operations) {
			t.Errorf("want %d operations, got %d", len(original.Operations), len(parsed.Operations))
		}
	})
	t.Run("distribution survives round-trip", func(t *testing.T) {
		arg := parsed.Operations["CreateOrder"].Arguments["currency"]
		if arg.Distribution["GBP"] != 0.70 {
			t.Errorf("GBP: want 0.70, got %f", arg.Distribution["GBP"])
		}
	})
}

func containsString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

Run and confirm tests fail:

```bash
go test ./pkg/model/... -run TestParseProfile -race -v
go test ./pkg/model/... -run TestTraceProfile -race -v
```

---

## Step 4 — `ComposedGenerator`

### Spec

- Package: `internal/strategy/property/gen`
- `ComposedGenerator` implements the three-rule composition from ADR-009
- `NewComposedGenerator(schema *openapi3.Schema, traceArg *model.ArgumentProfile) *ComposedGenerator`
- Rule selection is determined at construction time, not per-draw — the selected rule is logged at `DEBUG` level
- Schema validation of trace-observed values is performed at construction time for distribution rule (rule 1); invalid values are dropped with a `WARN` log
- If all observed values fail schema validation, falls back to rule 3 with a `WARN` log

### Tests

`internal/strategy/property/gen/composed_test.go`:

```go
package gen_test

import (
	"testing"

	rapid "pgregory.net/rapid"

	"github.com/yourorg/bt/internal/strategy/property/gen"
	"github.com/yourorg/bt/pkg/model"
)

func stringSchema() *openapi3.Schema {
	return &openapi3.Schema{Type: "string"}
}

func integerSchema() *openapi3.Schema {
	return &openapi3.Schema{Type: "integer"}
}

func stringEnumSchema(values []string) *openapi3.Schema {
	enum := make([]any, len(values))
	for i, v := range values {
		enum[i] = v
	}
	return &openapi3.Schema{Type: "string", Enum: enum}
}

func requiredStringSchema() *openapi3.Schema {
	return &openapi3.Schema{Type: "string", Nullable: false}
}

func optionalStringSchema() *openapi3.Schema {
	// Represents a field wrapped in rapid.Optional — simulated here as nullable
	return &openapi3.Schema{Type: "string", Nullable: true}
}

func repeat(values []string, times int) []any {
	result := make([]any, 0, len(values)*times)
	for i := 0; i < times; i++ {
		for _, v := range values {
			result = append(result, v)
		}
	}
	return result
}

func frequencyOf(value string, values []any) float64 {
	count := 0
	for _, v := range values {
		if v == value {
			count++
		}
	}
	return float64(count) / float64(len(values))
}

func uniqueCount(values []any) int {
	seen := map[any]bool{}
	for _, v := range values {
		seen[v] = true
	}
	return len(seen)
}

func drawN(t *testing.T, g *gen.ComposedGenerator, n int) []any {
	t.Helper()
	results := make([]any, 0, n)
	rapid.Check(t, func(t *rapid.T) {
		if len(results) >= n {
			return
		}
		results = append(results, g.Draw(t, "v"))
	})
	return results
}

func TestComposedGenerator_NoTraceData_GeneratesArbitraryStrings(t *testing.T) {
	g := gen.NewComposedGenerator(stringSchema(), nil)
	values := drawN(t, g, 100)
	if uniqueCount(values) <= 5 {
		t.Errorf("expected varied strings with no trace data, got only %d unique values", uniqueCount(values))
	}
}

func TestComposedGenerator_SufficientDistribution_DrawsFromDistribution(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:         "string",
		Samples:      repeat([]string{"GBP", "USD", "EUR"}, 10), // 30 samples
		Distribution: map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05},
	}
	g := gen.NewComposedGenerator(stringSchema(), traceArg)
	values := drawN(t, g, 1000)

	for _, v := range values {
		s := v.(string)
		if s != "GBP" && s != "USD" && s != "EUR" {
			t.Errorf("expected only distribution values, got %q", s)
		}
	}

	gbpFreq := frequencyOf("GBP", values)
	if gbpFreq < 0.55 || gbpFreq > 0.85 {
		t.Errorf("GBP frequency: want ~0.70 (±0.15), got %f", gbpFreq)
	}
}

func TestComposedGenerator_InsufficientSamples_FallsBackToSchema(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:    "string",
		Samples: []any{"GBP", "USD"}, // only 2 samples — below threshold
	}
	g := gen.NewComposedGenerator(stringSchema(), traceArg)
	values := drawN(t, g, 100)
	if uniqueCount(values) <= 2 {
		t.Errorf("insufficient samples should fall back to schema generator producing variety; got only %d unique values", uniqueCount(values))
	}
}

func TestComposedGenerator_AllObservedValuesInvalid_FallsBackToSchema(t *testing.T) {
	// Schema only allows GBP and USD; trace observed INVALID which is not in enum
	enumSchema := stringEnumSchema([]string{"GBP", "USD"})
	traceArg := &model.ArgumentProfile{
		Type:         "string",
		Samples:      repeat([]string{"INVALID"}, 30),
		Distribution: map[string]float64{"INVALID": 1.0},
	}
	g := gen.NewComposedGenerator(enumSchema, traceArg)
	values := drawN(t, g, 100)
	for _, v := range values {
		s := v.(string)
		if s != "GBP" && s != "USD" {
			t.Errorf("expected only schema-valid values after invalid trace drop; got %q", s)
		}
	}
}

func TestComposedGenerator_NumericRange_AppliedToIntegerSchema(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:  "integer",
		Range: &model.Range{Min: 10, Max: 500},
	}
	g := gen.NewComposedGenerator(integerSchema(), traceArg)

	rapid.Check(t, func(t *rapid.T) {
		val := g.Draw(t, "v")
		var n int64
		switch v := val.(type) {
		case int64:
			n = v
		case int32:
			n = int64(v)
		case float64:
			n = int64(v)
		default:
			t.Fatalf("expected integer-compatible type, got %T", val)
		}
		if n < 10 || n > 500 {
			t.Fatalf("value %d outside observed range [10, 500]", n)
		}
	})
}

func TestComposedGenerator_NullRateFromTrace_NonNullableFieldNeverNil(t *testing.T) {
	// Schema says non-nullable; trace observed 50% nulls → schema wins
	traceArg := &model.ArgumentProfile{
		Type:     "string",
		NullRate: 0.5,
		Samples:  repeat([]string{"GBP"}, 30),
	}
	g := gen.NewComposedGenerator(requiredStringSchema(), traceArg)
	values := drawN(t, g, 200)
	for _, v := range values {
		if v == nil {
			t.Error("non-nullable field must never be nil, even with high trace null rate")
		}
	}
}

func TestComposedGenerator_AlwaysPresent_RemovesOptionalWrapper(t *testing.T) {
	traceArg := &model.ArgumentProfile{
		Type:          "string",
		AlwaysPresent: true,
		Samples:       repeat([]string{"note"}, 30),
	}
	// optionalStringSchema is nullable — without AlwaysPresent, sometimes nil
	g := gen.NewComposedGenerator(optionalStringSchema(), traceArg)
	values := drawN(t, g, 200)
	for _, v := range values {
		if v == nil {
			t.Error("AlwaysPresent=true must prevent nil generation even on optional/nullable field")
		}
	}
}
```

Run and confirm tests fail:

```bash
go test ./internal/strategy/property/gen/... -run TestComposedGenerator -race -v
```

---

## Step 5 — CLI commands

### Spec

**`bt trace import <har-file>`**
- Reads and parses the HAR file
- Loads the OpenAPI spec from the config (`backendtest.yaml` or `--config` flag)
- Calls `analyze.Analyze` with the parsed entries and spec
- Writes the profile to the path configured in `backendtest.yaml` under `trace.profile` (default: `.bt/trace/profile.json`)
- Prints a summary on success: operation count, total call count, path written
- Exits 1 on any error with a clear message

**`bt trace inspect`**
- Reads the profile from `trace.profile` path in config
- Prints a human-readable summary:
  - Operations table: operation ID, call count, frequency rank
  - For each operation: argument name, type, sample count, distribution (if populated), range (if populated)
  - Sequences section: start probability table, top-3 transitions per operation
- Exits 1 if profile is missing or unparseable

### Tests

`internal/cli/trace_test.go`:

```go
package cli_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yourorg/bt/internal/cli"
)

func TestTraceImport_ValidHAR_WritesProfile(t *testing.T) {
	dir := t.TempDir()
	harPath := filepath.Join(dir, "sample.har")
	profilePath := filepath.Join(dir, ".bt", "trace", "profile.json")
	specPath := writeMinimalSpec(t, dir)
	cfgPath := writeConfig(t, dir, specPath, profilePath)

	writeHAR(t, harPath, makeOrdersHAR(30))

	var out bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"trace", "import", harPath, "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("trace import: %v", err)
	}

	t.Run("profile file is written to configured path", func(t *testing.T) {
		if _, err := os.Stat(profilePath); os.IsNotExist(err) {
			t.Errorf("expected profile at %q", profilePath)
		}
	})

	t.Run("profile file is valid JSON", func(t *testing.T) {
		data, _ := os.ReadFile(profilePath)
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Errorf("profile is not valid JSON: %v", err)
		}
	})

	t.Run("output mentions operation count", func(t *testing.T) {
		if !strings.Contains(out.String(), "operation") {
			t.Errorf("expected output to mention operations; got: %s", out.String())
		}
	})

	t.Run("output mentions profile path", func(t *testing.T) {
		if !strings.Contains(out.String(), ".bt/trace/profile.json") &&
			!strings.Contains(out.String(), profilePath) {
			t.Errorf("expected output to mention profile path; got: %s", out.String())
		}
	})
}

func TestTraceImport_MissingHARFile_ExitsWithError(t *testing.T) {
	dir := t.TempDir()
	specPath := writeMinimalSpec(t, dir)
	cfgPath := writeConfig(t, dir, specPath, "")

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"trace", "import", "/nonexistent/file.har", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for missing HAR file")
	}
}

func TestTraceImport_MalformedHAR_ExitsWithError(t *testing.T) {
	dir := t.TempDir()
	harPath := filepath.Join(dir, "bad.har")
	os.WriteFile(harPath, []byte("not json"), 0644)
	specPath := writeMinimalSpec(t, dir)
	cfgPath := writeConfig(t, dir, specPath, "")

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"trace", "import", harPath, "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for malformed HAR")
	}
}

func TestTraceInspect_ValidProfile_PrintsSummary(t *testing.T) {
	dir := t.TempDir()
	profilePath := filepath.Join(dir, "profile.json")
	specPath := writeMinimalSpec(t, dir)
	cfgPath := writeConfig(t, dir, specPath, profilePath)
	writeValidProfile(t, profilePath)

	var out bytes.Buffer
	cmd := cli.NewRootCmd()
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"trace", "inspect", "--config", cfgPath})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("trace inspect: %v", err)
	}

	output := out.String()

	t.Run("output contains operation name", func(t *testing.T) {
		if !strings.Contains(output, "CreateOrder") {
			t.Errorf("expected 'CreateOrder' in inspect output; got:\n%s", output)
		}
	})
	t.Run("output contains call count", func(t *testing.T) {
		if !strings.Contains(output, "30") {
			t.Errorf("expected call count in inspect output; got:\n%s", output)
		}
	})
	t.Run("output contains distribution data", func(t *testing.T) {
		if !strings.Contains(output, "GBP") {
			t.Errorf("expected distribution value 'GBP' in inspect output; got:\n%s", output)
		}
	})
	t.Run("output contains sequences section", func(t *testing.T) {
		if !strings.Contains(strings.ToLower(output), "sequence") {
			t.Errorf("expected sequences section in inspect output; got:\n%s", output)
		}
	})
}

func TestTraceInspect_MissingProfile_ExitsWithError(t *testing.T) {
	dir := t.TempDir()
	specPath := writeMinimalSpec(t, dir)
	cfgPath := writeConfig(t, dir, specPath, filepath.Join(dir, "nonexistent.json"))

	cmd := cli.NewRootCmd()
	cmd.SetArgs([]string{"trace", "inspect", "--config", cfgPath})

	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for missing profile")
	}
}

// --- helpers (stubs — fill in with real implementations) ---
func writeMinimalSpec(t *testing.T, dir string) string   { t.Helper(); return "" }
func writeConfig(t *testing.T, dir, spec, profile string) string { t.Helper(); return "" }
func writeHAR(t *testing.T, path string, data []byte)    { t.Helper() }
func makeOrdersHAR(n int) []byte                          { return nil }
func writeValidProfile(t *testing.T, path string)         { t.Helper() }
```

Run and confirm tests fail:

```bash
go test ./internal/cli/... -run TestTrace -race -v
```

---

## Step 6 — Property strategy wired to `ComposedGenerator`

### Spec

- The property strategy (`internal/strategy/property`) is updated to accept an optional `*model.TraceProfile` in its config
- When a profile is present, `Execute` uses `ComposedGenerator` for each argument; when absent, uses the existing schema-only generator unchanged
- Profile loading happens at strategy startup (once per run), not per case
- The `backendtest.yaml` `trace.profile` key is parsed and passed to the property strategy via the existing config pipeline

### Tests

`internal/strategy/property/trace_integration_test.go`:

```go
package property_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/yourorg/bt/internal/strategy/property"
	"github.com/yourorg/bt/pkg/model"
)

// captureServer records all request bodies sent to it.
type captureServer struct {
	mu      sync.Mutex
	bodies  [][]byte
	handler http.Handler
}

func newCaptureServer(statusCode int, respBody string) *captureServer {
	cs := &captureServer{}
	cs.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		cs.mu.Lock()
		cs.bodies = append(cs.bodies, buf.Bytes())
		cs.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		w.Write([]byte(respBody))
	})
	return cs
}

func TestPropertyStrategy_WithTraceProfile_UsesDistributionForCurrency(t *testing.T) {
	// Profile says currency is GBP 70%, USD 25%, EUR 5% with 30 samples
	profile := &model.TraceProfile{
		SchemaVersion: "1",
		GeneratedAt:   "2024-01-15T10:00:00Z",
		Operations: map[string]*model.OperationProfile{
			"CreateOrder": {
				CallCount:     30,
				FrequencyRank: 1,
				Arguments: map[string]*model.ArgumentProfile{
					"currency": {
						Type:         "string",
						Samples:      repeat([]string{"GBP", "USD", "EUR"}, 10),
						Distribution: map[string]float64{"GBP": 0.70, "USD": 0.25, "EUR": 0.05},
						AlwaysPresent: true,
					},
				},
			},
		},
	}

	cs := newCaptureServer(201, `{"id":"ord_1","status":"pending"}`)
	srv := httptest.NewServer(cs.handler)
	defer srv.Close()

	runner := property.NewRunner(property.Config{
		BaseURL:      srv.URL,
		Checks:       200,
		TraceProfile: profile,
	})

	op := createOrderOp() // model.Operation for CreateOrder with currency: string
	cases, _ := runner.Plan(context.Background(), model.StrategySpec{}, []model.Operation{op})
	runner.Execute(context.Background(), cases, nil)

	// Extract currency values from captured request bodies
	currencies := extractCurrencyValues(t, cs.bodies)

	t.Run("at least 100 requests captured", func(t *testing.T) {
		if len(currencies) < 100 {
			t.Errorf("expected at least 100 captured requests, got %d", len(currencies))
		}
	})

	t.Run("all currency values are from the trace distribution", func(t *testing.T) {
		for _, c := range currencies {
			if c != "GBP" && c != "USD" && c != "EUR" {
				t.Errorf("unexpected currency value %q — not in trace distribution", c)
			}
		}
	})

	t.Run("GBP is the most frequent currency (>50%)", func(t *testing.T) {
		if len(currencies) == 0 {
			t.Skip("no currencies captured")
		}
		gbpCount := 0
		for _, c := range currencies {
			if c == "GBP" {
				gbpCount++
			}
		}
		freq := float64(gbpCount) / float64(len(currencies))
		if freq < 0.50 {
			t.Errorf("GBP frequency: want >0.50, got %f", freq)
		}
	})
}

func TestPropertyStrategy_WithoutTraceProfile_BehavesAsM4(t *testing.T) {
	// No trace profile — must behave identically to M4 pure-schema generator
	cs := newCaptureServer(201, `{"id":"ord_1","status":"pending"}`)
	srv := httptest.NewServer(cs.handler)
	defer srv.Close()

	runner := property.NewRunner(property.Config{
		BaseURL:      srv.URL,
		Checks:       100,
		TraceProfile: nil, // no profile
	})

	op := createOrderOp()
	cases, _ := runner.Plan(context.Background(), model.StrategySpec{}, []model.Operation{op})
	results, err := runner.Execute(context.Background(), cases, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// All cases should pass — the server accepts any valid input
	for _, r := range results {
		if !r.Passed {
			t.Errorf("case %q should pass without trace profile; failures: %v", r.ID, r.Failures)
		}
	}
}
```

Run and confirm tests fail:

```bash
go test ./internal/strategy/property/... -run TestPropertyStrategy_WithTraceProfile -race -v
```

---

## Implementation

Only begin once all tests are written and confirmed failing.

### Recommended build order

1. **`pkg/model/trace.go`** — define all types; implement `ParseProfile`, `WriteToFile`, error types, and validation. No external dependencies except standard library and `encoding/json`.

2. **`internal/trace/har`** — implement `Parse`, `Filter`, `HAR`, `HAREntry`, error types. Dependency: standard library only.

3. **`internal/trace/analyze`** — implement `Analyze`. Dependencies: `har`, `pkg/model`, `openapi3` (for operation matching). Session grouping: 30-minute window, same host IP. Operation matching: compare `entry.Method + urlPath` against spec `paths` using path template matching.

4. **`internal/strategy/property/gen/composed.go`** — implement `ComposedGenerator` and `NewComposedGenerator`. Dependencies: `pkg/model`, `openapi3`, `pgregory.net/rapid`. Rule selection at construction time; `Draw` is a simple dispatch.

5. **Property strategy wiring** — update `internal/strategy/property/runner.go` to accept `TraceProfile` in config and pass `ArgumentProfile` to `NewComposedGenerator` per argument.

6. **CLI commands** — add `bt trace` subcommand with `import` and `inspect` children. `import` is the main pipeline; `inspect` is a read-only pretty-printer.

---

## Full verification

```bash
# Unit tests for this milestone
go test ./internal/trace/har/... -race -v
go test ./internal/trace/analyze/... -race -v
go test ./pkg/model/... -run "TestParseProfile|TestTraceProfile" -race -v
go test ./internal/strategy/property/gen/... -run TestComposedGenerator -race -v
go test ./internal/strategy/property/... -run TestPropertyStrategy_WithTrace -race -v
go test ./internal/cli/... -run TestTrace -race -v

# Full suite — must not regress
go test ./... -race

# Lint
golangci-lint run ./...

# Build
CGO_ENABLED=0 go build ./cmd/bt
```

---

## M12 exit criterion

1. `har.Parse` handles valid HAR 1.2 (and 1.1), non-JSON bodies, missing responses, empty entries, malformed JSON, and unsupported versions — each with a distinct typed error
2. `analyze.Analyze` produces correct `CallCount`, `FrequencyRank`, `Distribution` (when ≥20 samples), `Range`, `NullRate`, `AlwaysPresent`, and Markov chain `Sequences`; all transition rows sum to 1.0 ± 0.001
3. `ParseProfile` / `WriteToFile` round-trip correctly; all five error conditions return the correct typed error
4. `ComposedGenerator` applies the three ADR-009 rules correctly; non-nullable fields are never nil regardless of trace null rate; insufficient samples fall back to schema
5. Property strategy with trace profile generates currency values matching the HAR distribution (GBP >50% over 200 draws)
6. `bt trace import` writes a valid profile; `bt trace inspect` prints operation, distribution, and sequence data
7. All tests pass with `-race`