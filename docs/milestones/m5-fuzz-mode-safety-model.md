# M5 — Fuzz Mode + Safety Model

This document follows the same structure as M1–M4: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist. Tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

---

## Overview

M5 delivers mutation-heavy robustness testing using Go's native fuzzing (`go test -fuzz`), paired with a safety model that makes it safe to run in CI by default. Where M4 generates *valid structured inputs* and checks invariants, M5 generates *malformed, hostile, and boundary-pushing inputs* and checks for crashes, panics, timeouts, and unexpected behaviour.

The safety model is not an afterthought — it is designed first, because fuzz mode without it would be unsafe to run against anything but a fully isolated environment.

The five pieces built here are:

1. **Safety model** — profiles (`safe`, `aggressive`, `destructive`), method allow/deny lists, per-operation throttling, and explicit opt-in enforcement for destructive modes
2. **Mutators** — payload, header, path, and query string mutators that produce hostile inputs from a seed corpus
3. **Corpus manager** — loads seed inputs from prior property failures and manually curated cases; writes interesting inputs back to the corpus
4. **Fuzz runner** — integrates with `go test -fuzz`, manages the lifecycle, and surfaces results through the shared `Result` and `Artifact` model
5. **Failure classifier** — categorises failures as: `crash`, `timeout`, `validation_leak`, `schema_break`, or `unexpected_status`

Each piece has its own spec, tests, and implementation section. Build and verify in order.

**Exit criterion:** `bt run --strategy fuzz --safety safe` runs in CI without manual guards and produces classified failure reports. Destructive methods (`DELETE`, `PUT` by default) are blocked unless `--safety destructive` is explicitly passed. Every failure produces an artifact that can be replayed exactly.

---

## Step 1 — Safety model

### Spec

The safety model is the first thing the fuzz runner consults before sending any request. It lives at `internal/strategy/fuzz/safety`.

- `Profile` is a string enum: `"safe"`, `"aggressive"`, `"destructive"`
- Default profile is `"safe"` — it must never need to be specified explicitly for CI to be protected
- `SafetyConfig` holds the complete safety configuration:
  ```
  Profile        Profile
  AllowedMethods []string   // methods explicitly permitted; empty = use profile defaults
  DeniedMethods  []string   // methods explicitly blocked, always wins over AllowedMethods
  MaxRequestsPerSecond float64
  MaxConcurrency  int
  TimeoutSeconds  float64
  ```
- Profile defaults (applied when `AllowedMethods` is empty):
  - `safe`: GET, POST, PATCH — never DELETE, PUT, HEAD, OPTIONS
  - `aggressive`: GET, POST, PATCH, PUT — never DELETE
  - `destructive`: all methods permitted — requires `--safety destructive` explicit flag; cannot be set via config file alone
- `Enforcer` is the runtime gatekeeper:
  - `Allow(method string) bool` — returns true if the method is permitted under the current config
  - `ThrottleDelay() time.Duration` — returns the minimum delay between requests based on `MaxRequestsPerSecond`
  - `RequestTimeout() time.Duration` — returns the per-request timeout
- `NewEnforcer(cfg SafetyConfig) (*Enforcer, error)` — validates the config and returns an enforcer; returns an error if `Profile` is `"destructive"` and the config was not constructed with `WithDestructiveConfirmed(true)`
- The `destructive` profile requires a second confirmation: the `WithDestructiveConfirmed(bool)` option must be passed as `true` — this represents the explicit CLI flag `--safety destructive` which cannot be expressed in a config file
- An enforcer built from a config file alone can never be destructive — the flag must come from the CLI layer
- `Enforcer` is safe for concurrent use

### Tests

`internal/strategy/fuzz/safety/safety_test.go`:

```go
package safety_test

import (
	"testing"
	"time"

	"github.com/jimbery/bt/internal/strategy/fuzz/safety"
)

// --- Profile defaults ---

func TestEnforcer_SafeProfile_AllowsGET(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if !e.Allow("GET") {
		t.Error("safe profile must allow GET")
	}
}

func TestEnforcer_SafeProfile_AllowsPOST(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if !e.Allow("POST") {
		t.Error("safe profile must allow POST")
	}
}

func TestEnforcer_SafeProfile_AllowsPATCH(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if !e.Allow("PATCH") {
		t.Error("safe profile must allow PATCH")
	}
}

func TestEnforcer_SafeProfile_BlocksDELETE(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if e.Allow("DELETE") {
		t.Error("safe profile must block DELETE")
	}
}

func TestEnforcer_SafeProfile_BlocksPUT(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if e.Allow("PUT") {
		t.Error("safe profile must block PUT")
	}
}

func TestEnforcer_SafeProfile_BlocksHEAD(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	if e.Allow("HEAD") {
		t.Error("safe profile must block HEAD")
	}
}

func TestEnforcer_AggressiveProfile_AllowsPUT(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileAggressive})
	if !e.Allow("PUT") {
		t.Error("aggressive profile must allow PUT")
	}
}

func TestEnforcer_AggressiveProfile_BlocksDELETE(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileAggressive})
	if e.Allow("DELETE") {
		t.Error("aggressive profile must block DELETE")
	}
}

func TestEnforcer_DestructiveProfile_RequiresExplicitConfirmation(t *testing.T) {
	// Building a destructive enforcer without WithDestructiveConfirmed must fail.
	_, err := safety.NewEnforcer(safety.SafetyConfig{Profile: safety.ProfileDestructive})
	if err == nil {
		t.Error("expected error when building destructive enforcer without explicit confirmation")
	}
}

func TestEnforcer_DestructiveProfile_WithConfirmation_AllowsDELETE(t *testing.T) {
	e, err := safety.NewEnforcer(
		safety.SafetyConfig{Profile: safety.ProfileDestructive},
		safety.WithDestructiveConfirmed(true),
	)
	if err != nil {
		t.Fatalf("unexpected error with destructive confirmation: %v", err)
	}
	if !e.Allow("DELETE") {
		t.Error("destructive profile with confirmation must allow DELETE")
	}
}

// --- Allow/Deny overrides ---

func TestEnforcer_AllowedMethods_OverridesProfileDefaults(t *testing.T) {
	// Explicitly allow only GET — even POST (which safe allows) is blocked.
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:        safety.ProfileSafe,
		AllowedMethods: []string{"GET"},
	})
	if e.Allow("POST") {
		t.Error("POST should be blocked when not in AllowedMethods override")
	}
	if !e.Allow("GET") {
		t.Error("GET should be allowed when in AllowedMethods override")
	}
}

func TestEnforcer_DeniedMethods_WinsOverAllowedMethods(t *testing.T) {
	// DeniedMethods always wins, even if the method appears in AllowedMethods.
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:        safety.ProfileSafe,
		AllowedMethods: []string{"GET", "POST"},
		DeniedMethods:  []string{"POST"},
	})
	if e.Allow("POST") {
		t.Error("POST must be blocked when it appears in DeniedMethods, even if in AllowedMethods")
	}
	if !e.Allow("GET") {
		t.Error("GET must be allowed when in AllowedMethods and not in DeniedMethods")
	}
}

func TestEnforcer_DeniedMethods_WinsOverProfileDefaults(t *testing.T) {
	// Even a method the profile allows can be explicitly denied.
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:       safety.ProfileSafe,
		DeniedMethods: []string{"GET"},
	})
	if e.Allow("GET") {
		t.Error("GET must be blocked when explicitly in DeniedMethods")
	}
}

func TestEnforcer_MethodCheck_IsCaseInsensitive(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	// Methods should be checked case-insensitively.
	if !e.Allow("get") {
		t.Error("Allow should be case-insensitive: 'get' should be treated as 'GET'")
	}
	if e.Allow("delete") {
		t.Error("Allow should be case-insensitive: 'delete' should be treated as 'DELETE' and blocked")
	}
}

// --- Throttle delay ---

func TestEnforcer_ThrottleDelay_ZeroRPS_ReturnsZero(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:             safety.ProfileSafe,
		MaxRequestsPerSecond: 0,
	})
	if d := e.ThrottleDelay(); d != 0 {
		t.Errorf("expected zero delay when MaxRequestsPerSecond is 0, got %v", d)
	}
}

func TestEnforcer_ThrottleDelay_10RPS_Returns100ms(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:             safety.ProfileSafe,
		MaxRequestsPerSecond: 10,
	})
	expected := 100 * time.Millisecond
	if d := e.ThrottleDelay(); d != expected {
		t.Errorf("expected %v for 10 RPS, got %v", expected, d)
	}
}

func TestEnforcer_ThrottleDelay_1RPS_Returns1Second(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:             safety.ProfileSafe,
		MaxRequestsPerSecond: 1,
	})
	expected := 1 * time.Second
	if d := e.ThrottleDelay(); d != expected {
		t.Errorf("expected %v for 1 RPS, got %v", expected, d)
	}
}

// --- Request timeout ---

func TestEnforcer_RequestTimeout_Default_Is30Seconds(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	expected := 30 * time.Second
	if to := e.RequestTimeout(); to != expected {
		t.Errorf("expected default timeout %v, got %v", expected, to)
	}
}

func TestEnforcer_RequestTimeout_CustomValue_IsRespected(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{
		Profile:        safety.ProfileSafe,
		TimeoutSeconds: 5,
	})
	expected := 5 * time.Second
	if to := e.RequestTimeout(); to != expected {
		t.Errorf("expected %v, got %v", expected, to)
	}
}

// --- Config validation ---

func TestNewEnforcer_NegativeRPS_ReturnsError(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{
		Profile:             safety.ProfileSafe,
		MaxRequestsPerSecond: -1,
	})
	if err == nil {
		t.Error("expected error for negative MaxRequestsPerSecond")
	}
}

func TestNewEnforcer_NegativeTimeout_ReturnsError(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{
		Profile:        safety.ProfileSafe,
		TimeoutSeconds: -5,
	})
	if err == nil {
		t.Error("expected error for negative TimeoutSeconds")
	}
}

func TestNewEnforcer_UnknownProfile_ReturnsError(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{Profile: "yolo"})
	if err == nil {
		t.Error("expected error for unknown profile")
	}
}

func TestNewEnforcer_EmptyProfile_ReturnsError(t *testing.T) {
	_, err := safety.NewEnforcer(safety.SafetyConfig{})
	if err == nil {
		t.Error("expected error for empty profile")
	}
}

// --- Concurrency safety ---

func TestEnforcer_Allow_IsSafeForConcurrentUse(t *testing.T) {
	e := mustEnforcer(t, safety.SafetyConfig{Profile: safety.ProfileSafe})
	done := make(chan struct{})
	for i := 0; i < 100; i++ {
		go func() {
			e.Allow("GET")
			e.Allow("DELETE")
			done <- struct{}{}
		}()
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}

// --- Helper ---

func mustEnforcer(t *testing.T, cfg safety.SafetyConfig, opts ...safety.Option) *safety.Enforcer {
	t.Helper()
	e, err := safety.NewEnforcer(cfg, opts...)
	if err != nil {
		t.Fatalf("unexpected error building enforcer: %v", err)
	}
	return e
}
```

---

## Step 2 — Mutators

### Spec

Mutators live at `internal/strategy/fuzz/mutate`. They take a seed input and produce a stream of mutated variants. Mutators are the core of what makes fuzz testing different from property testing — they do not generate from a schema, they *corrupt* existing inputs.

- `Mutator` interface:
  ```go
  type Mutator interface {
      Name() string
      Mutate(seed Input, r *rand.Rand) Input
  }
  ```
- `Input` carries all mutable parts of a request:
  ```go
  type Input struct {
      Method  string
      Path    string            // already path-resolved, e.g. /orders/ord-001
      Query   map[string]string
      Headers map[string]string
      Body    []byte
  }
  ```
- Four mutators are implemented:

**`PayloadMutator`** — mutates the request body:
  - `truncate`: removes a random suffix of bytes
  - `inject_null_byte`: inserts a null byte at a random position
  - `flip_bit`: flips a random bit in the body
  - `replace_value`: replaces a random JSON string value with one of: `""`, `null`, `true`, `false`, `0`, `9999999`, a 10,000-character string, a string of only whitespace, a unicode edge-case string (`\u0000`, surrogates, overlong sequences)
  - `duplicate_key`: duplicates a random JSON object key
  - `strip_content_type`: removes the `Content-Type` header
  - On empty body: returns the seed unchanged (no panic)

**`HeaderMutator`** — mutates request headers:
  - `drop_header`: removes a random header from the map
  - `empty_value`: sets a random header's value to `""`
  - `oversized_value`: sets a random header's value to a 8192-character string
  - `inject_newline`: injects `\r\n` into a random header value
  - `add_unknown_header`: adds `X-Fuzz-Unknown: fuzz` to every mutated request
  - On empty headers map: returns seed with only `X-Fuzz-Unknown` added

**`PathMutator`** — mutates path segments:
  - `empty_segment`: replaces a random path segment with `""`
  - `oversized_segment`: replaces a random path segment with a 2048-character string
  - `inject_traversal`: replaces a random segment with `../..`
  - `inject_special_chars`: replaces a random segment with `%00`, `<script>`, `'OR 1=1--`
  - `append_extension`: appends `.json`, `.php`, `.xml`, `.env` to the path
  - On paths with no segments (just `"/"`): returns seed unchanged

**`QueryMutator`** — mutates query parameters:
  - `drop_param`: removes a random query parameter
  - `empty_value`: sets a random parameter's value to `""`
  - `oversized_value`: sets a random parameter's value to a 4096-character string
  - `inject_special_chars`: sets a random parameter's value to `'OR 1=1--`, `<script>alert(1)</script>`, `%00`
  - `duplicate_param`: duplicates a random parameter (appends it again)
  - On empty query: adds `fuzz=1` parameter

- Each mutator's `Mutate` method must never panic, even on nil or empty input fields
- Mutation is deterministic for a given seed and `*rand.Rand` state — the same rand produces the same mutation
- `MutatorSet` is a collection of mutators with a `MutateAll(seed Input, r *rand.Rand) []Input` method that applies every mutator once and returns all variants
- The order of variants returned by `MutateAll` is stable (same order as mutators were registered)

### Tests

`internal/strategy/fuzz/mutate/mutate_test.go`:

```go
package mutate_test

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/strategy/fuzz/mutate"
)

// --- Helpers ---

func rng(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

func validJSONInput() mutate.Input {
	return mutate.Input{
		Method: "POST",
		Path:   "/orders/ord-001",
		Query:  map[string]string{"status": "pending"},
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Authorization": "Bearer token",
		},
		Body: []byte(`{"amount":99.99,"currency":"GBP","status":"pending"}`),
	}
}

func emptyInput() mutate.Input {
	return mutate.Input{
		Method:  "GET",
		Path:    "/",
		Query:   map[string]string{},
		Headers: map[string]string{},
		Body:    []byte{},
	}
}

// --- PayloadMutator ---

func TestPayloadMutator_Name_IsNonEmpty(t *testing.T) {
	m := mutate.NewPayloadMutator()
	if m.Name() == "" {
		t.Error("mutator Name() must not be empty")
	}
}

func TestPayloadMutator_Mutate_ReturnsInput(t *testing.T) {
	m := mutate.NewPayloadMutator()
	result := m.Mutate(validJSONInput(), rng(1))
	// Must return a valid Input struct, not panic.
	_ = result
}

func TestPayloadMutator_Mutate_DoesNotPanicOnEmptyBody(t *testing.T) {
	m := mutate.NewPayloadMutator()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PayloadMutator panicked on empty body: %v", r)
		}
	}()
	m.Mutate(emptyInput(), rng(2))
}

func TestPayloadMutator_Mutate_DoesNotPanicOnNilBody(t *testing.T) {
	m := mutate.NewPayloadMutator()
	input := validJSONInput()
	input.Body = nil
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PayloadMutator panicked on nil body: %v", r)
		}
	}()
	m.Mutate(input, rng(3))
}

func TestPayloadMutator_Mutate_ProducesChangedBody_OverManyRuns(t *testing.T) {
	// Over 20 seeds, at least one must produce a body different from the original.
	m := mutate.NewPayloadMutator()
	original := validJSONInput()
	changed := false
	for seed := int64(0); seed < 20; seed++ {
		result := m.Mutate(original, rng(seed))
		if string(result.Body) != string(original.Body) {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("PayloadMutator never changed the body over 20 seeds")
	}
}

func TestPayloadMutator_Mutate_IsDeterministic(t *testing.T) {
	// Same rand seed must produce same result.
	m := mutate.NewPayloadMutator()
	input := validJSONInput()
	r1 := m.Mutate(input, rng(42))
	r2 := m.Mutate(input, rng(42))
	if string(r1.Body) != string(r2.Body) {
		t.Error("PayloadMutator must be deterministic for the same rand seed")
	}
}

func TestPayloadMutator_Truncate_ProducesShorterBody(t *testing.T) {
	// Run enough seeds to hit the truncate mutation.
	m := mutate.NewPayloadMutator()
	input := validJSONInput()
	truncated := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		if len(result.Body) < len(input.Body) {
			truncated = true
			break
		}
	}
	if !truncated {
		t.Error("PayloadMutator never produced a truncated body over 50 seeds")
	}
}

func TestPayloadMutator_StripContentType_RemovesHeader(t *testing.T) {
	m := mutate.NewPayloadMutator()
	input := validJSONInput()
	stripped := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		if _, present := result.Headers["Content-Type"]; !present {
			stripped = true
			break
		}
	}
	if !stripped {
		t.Error("PayloadMutator never stripped Content-Type over 50 seeds")
	}
}

// --- HeaderMutator ---

func TestHeaderMutator_Name_IsNonEmpty(t *testing.T) {
	m := mutate.NewHeaderMutator()
	if m.Name() == "" {
		t.Error("mutator Name() must not be empty")
	}
}

func TestHeaderMutator_Mutate_DoesNotPanicOnEmptyHeaders(t *testing.T) {
	m := mutate.NewHeaderMutator()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("HeaderMutator panicked on empty headers: %v", r)
		}
	}()
	m.Mutate(emptyInput(), rng(1))
}

func TestHeaderMutator_Mutate_EmptyHeaders_AddsUnknownHeader(t *testing.T) {
	m := mutate.NewHeaderMutator()
	result := m.Mutate(emptyInput(), rng(1))
	if _, ok := result.Headers["X-Fuzz-Unknown"]; !ok {
		t.Error("HeaderMutator must add X-Fuzz-Unknown header when input headers are empty")
	}
}

func TestHeaderMutator_Mutate_AlwaysAddsUnknownHeader(t *testing.T) {
	m := mutate.NewHeaderMutator()
	// X-Fuzz-Unknown should appear in every mutated output.
	input := validJSONInput()
	for seed := int64(0); seed < 20; seed++ {
		result := m.Mutate(input, rng(seed))
		if _, ok := result.Headers["X-Fuzz-Unknown"]; !ok {
			t.Errorf("HeaderMutator missing X-Fuzz-Unknown for seed %d", seed)
		}
	}
}

func TestHeaderMutator_OversizedValue_Produces8192CharValue(t *testing.T) {
	m := mutate.NewHeaderMutator()
	input := validJSONInput()
	found := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		for _, v := range result.Headers {
			if len(v) == 8192 {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("HeaderMutator never produced an 8192-character header value over 50 seeds")
	}
}

func TestHeaderMutator_InjectNewline_ProducesNewlineInValue(t *testing.T) {
	m := mutate.NewHeaderMutator()
	input := validJSONInput()
	found := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		for _, v := range result.Headers {
			if strings.Contains(v, "\r\n") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("HeaderMutator never injected \\r\\n into a header value over 50 seeds")
	}
}

func TestHeaderMutator_Mutate_DoesNotModifyOriginalInput(t *testing.T) {
	m := mutate.NewHeaderMutator()
	input := validJSONInput()
	originalHeaderCount := len(input.Headers)
	m.Mutate(input, rng(1))
	// Original input must not be modified.
	if len(input.Headers) != originalHeaderCount {
		t.Error("HeaderMutator must not modify the original input's headers map")
	}
}

// --- PathMutator ---

func TestPathMutator_Name_IsNonEmpty(t *testing.T) {
	m := mutate.NewPathMutator()
	if m.Name() == "" {
		t.Error("mutator Name() must not be empty")
	}
}

func TestPathMutator_Mutate_DoesNotPanicOnRootPath(t *testing.T) {
	m := mutate.NewPathMutator()
	input := emptyInput() // path is "/"
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("PathMutator panicked on root path: %v", r)
		}
	}()
	m.Mutate(input, rng(1))
}

func TestPathMutator_Mutate_ProducesChangedPath_OverManyRuns(t *testing.T) {
	m := mutate.NewPathMutator()
	input := validJSONInput()
	changed := false
	for seed := int64(0); seed < 20; seed++ {
		result := m.Mutate(input, rng(seed))
		if result.Path != input.Path {
			changed = true
			break
		}
	}
	if !changed {
		t.Error("PathMutator never changed the path over 20 seeds")
	}
}

func TestPathMutator_InjectTraversal_ProducesDoubleDot(t *testing.T) {
	m := mutate.NewPathMutator()
	input := validJSONInput()
	found := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		if strings.Contains(result.Path, "..") {
			found = true
			break
		}
	}
	if !found {
		t.Error("PathMutator never injected a traversal sequence over 50 seeds")
	}
}

func TestPathMutator_AppendExtension_AppendsToPath(t *testing.T) {
	m := mutate.NewPathMutator()
	input := validJSONInput()
	found := false
	knownExtensions := []string{".json", ".php", ".xml", ".env"}
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		for _, ext := range knownExtensions {
			if strings.HasSuffix(result.Path, ext) {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("PathMutator never appended a file extension over 50 seeds")
	}
}

func TestPathMutator_Mutate_DoesNotModifyOriginalInput(t *testing.T) {
	m := mutate.NewPathMutator()
	input := validJSONInput()
	original := input.Path
	m.Mutate(input, rng(1))
	if input.Path != original {
		t.Error("PathMutator must not modify the original input's Path field")
	}
}

// --- QueryMutator ---

func TestQueryMutator_Name_IsNonEmpty(t *testing.T) {
	m := mutate.NewQueryMutator()
	if m.Name() == "" {
		t.Error("mutator Name() must not be empty")
	}
}

func TestQueryMutator_Mutate_DoesNotPanicOnEmptyQuery(t *testing.T) {
	m := mutate.NewQueryMutator()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("QueryMutator panicked on empty query: %v", r)
		}
	}()
	m.Mutate(emptyInput(), rng(1))
}

func TestQueryMutator_Mutate_EmptyQuery_AddsFuzzParam(t *testing.T) {
	m := mutate.NewQueryMutator()
	result := m.Mutate(emptyInput(), rng(1))
	if _, ok := result.Query["fuzz"]; !ok {
		t.Error("QueryMutator must add 'fuzz' param when input query is empty")
	}
}

func TestQueryMutator_InjectSpecialChars_ProducesSQLInjection(t *testing.T) {
	m := mutate.NewQueryMutator()
	input := validJSONInput()
	found := false
	for seed := int64(0); seed < 50; seed++ {
		result := m.Mutate(input, rng(seed))
		for _, v := range result.Query {
			if strings.Contains(v, "OR 1=1") || strings.Contains(v, "<script>") || strings.Contains(v, "%00") {
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Error("QueryMutator never produced a special-character query value over 50 seeds")
	}
}

func TestQueryMutator_Mutate_DoesNotModifyOriginalInput(t *testing.T) {
	m := mutate.NewQueryMutator()
	input := validJSONInput()
	original := map[string]string{}
	for k, v := range input.Query {
		original[k] = v
	}
	m.Mutate(input, rng(1))
	for k, v := range original {
		if input.Query[k] != v {
			t.Errorf("QueryMutator modified original query key %q", k)
		}
	}
}

// --- MutatorSet ---

func TestMutatorSet_MutateAll_ReturnsOneResultPerMutator(t *testing.T) {
	set := mutate.NewMutatorSet(
		mutate.NewPayloadMutator(),
		mutate.NewHeaderMutator(),
		mutate.NewPathMutator(),
		mutate.NewQueryMutator(),
	)
	results := set.MutateAll(validJSONInput(), rng(1))
	if len(results) != 4 {
		t.Errorf("expected 4 results (one per mutator), got %d", len(results))
	}
}

func TestMutatorSet_MutateAll_ResultOrderMatchesMutatorOrder(t *testing.T) {
	// Payload is first, header second. We can identify them by checking which field changed.
	payload := mutate.NewPayloadMutator()
	header := mutate.NewHeaderMutator()
	set := mutate.NewMutatorSet(payload, header)
	input := validJSONInput()
	results := set.MutateAll(input, rng(99))

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// The second result (header mutator) must always have X-Fuzz-Unknown.
	if _, ok := results[1].Headers["X-Fuzz-Unknown"]; !ok {
		t.Error("second result should come from HeaderMutator and have X-Fuzz-Unknown")
	}
}

func TestMutatorSet_MutateAll_DoesNotPanicOnEmptyInput(t *testing.T) {
	set := mutate.NewMutatorSet(
		mutate.NewPayloadMutator(),
		mutate.NewHeaderMutator(),
		mutate.NewPathMutator(),
		mutate.NewQueryMutator(),
	)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("MutatorSet.MutateAll panicked on empty input: %v", r)
		}
	}()
	set.MutateAll(emptyInput(), rng(1))
}

func TestMutatorSet_MutateAll_IsDeterministic(t *testing.T) {
	set := mutate.NewMutatorSet(
		mutate.NewPayloadMutator(),
		mutate.NewHeaderMutator(),
	)
	input := validJSONInput()
	r1 := set.MutateAll(input, rng(77))
	r2 := set.MutateAll(input, rng(77))
	for i := range r1 {
		if string(r1[i].Body) != string(r2[i].Body) {
			t.Errorf("result[%d] body differs between identical seeds", i)
		}
	}
}
```

---

## Step 3 — Corpus manager

### Spec

The corpus manager lives at `internal/strategy/fuzz/corpus`. It is responsible for loading seed inputs for a fuzz run and persisting newly discovered interesting inputs.

- `Corpus` holds a set of `Input` entries keyed by a content hash
- `NewCorpus(dir string) *Corpus` — creates a corpus backed by `dir`; the directory is created on first write if it does not exist
- `Load() ([]mutate.Input, error)` — reads all `.json` files from `dir` and deserialises them as `mutate.Input`; skips files that fail to parse (logs a warning, does not error)
- `Save(input mutate.Input) error` — serialises `input` to a JSON file in `dir`, named by the SHA-256 hash of the serialised content; if the file already exists (same hash), it is a no-op
- `Size() int` — returns the number of currently loaded entries
- Corpus entries written by M3/M4 artifact bundles are in a different format; the corpus manager reads only files it wrote itself (containing `mutate.Input` shaped JSON)
- The corpus directory for the orders API example is `examples/orders-api/bt/corpus/`
- When no corpus directory is configured, the runner uses a built-in minimal seed corpus (one entry per HTTP method in the safety profile)

### Tests

`internal/strategy/fuzz/corpus/corpus_test.go`:

```go
package corpus_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/jimbery/bt/internal/strategy/fuzz/corpus"
	"github.com/jimbery/bt/internal/strategy/fuzz/mutate"
)

func sampleInput(body string) mutate.Input {
	return mutate.Input{
		Method:  "POST",
		Path:    "/orders",
		Query:   map[string]string{},
		Headers: map[string]string{"Content-Type": "application/json"},
		Body:    []byte(body),
	}
}

// --- Load ---

func TestCorpus_Load_EmptyDir_ReturnsEmptySlice(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	entries, err := c.Load()
	if err != nil {
		t.Fatalf("Load returned unexpected error on empty dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries from empty dir, got %d", len(entries))
	}
}

func TestCorpus_Load_ReadsValidJSONFiles(t *testing.T) {
	dir := t.TempDir()
	input := sampleInput(`{"amount":10,"currency":"GBP"}`)
	data, _ := json.Marshal(input)
	if err := os.WriteFile(filepath.Join(dir, "entry.json"), data, 0644); err != nil {
		t.Fatalf("cannot write test file: %v", err)
	}

	c := corpus.NewCorpus(dir)
	entries, err := c.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry, got %d", len(entries))
	}
}

func TestCorpus_Load_SkipsMalformedFiles(t *testing.T) {
	dir := t.TempDir()
	// Write a valid entry and a malformed entry.
	input := sampleInput(`{"amount":1}`)
	data, _ := json.Marshal(input)
	os.WriteFile(filepath.Join(dir, "valid.json"), data, 0644)
	os.WriteFile(filepath.Join(dir, "broken.json"), []byte("not json"), 0644)

	c := corpus.NewCorpus(dir)
	entries, err := c.Load()
	if err != nil {
		t.Fatalf("Load should not error on malformed files, got: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("expected 1 entry (malformed file skipped), got %d", len(entries))
	}
}

func TestCorpus_Load_IgnoresNonJSONFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a corpus entry"), 0644)

	c := corpus.NewCorpus(dir)
	entries, err := c.Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries (only .json files loaded), got %d", len(entries))
	}
}

// --- Save ---

func TestCorpus_Save_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "corpus")
	c := corpus.NewCorpus(dir)
	if err := c.Save(sampleInput(`{"amount":1}`)); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Save must create the corpus directory if it does not exist")
	}
}

func TestCorpus_Save_WritesJSONFile(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	if err := c.Save(sampleInput(`{"amount":2}`)); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}
	entries, _ := c.Load()
	if len(entries) != 1 {
		t.Errorf("expected 1 entry after Save, got %d", len(entries))
	}
}

func TestCorpus_Save_SameContent_IsIdempotent(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	input := sampleInput(`{"amount":3}`)
	c.Save(input)
	c.Save(input) // identical content — should not create a second file
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Errorf("expected 1 file after saving same content twice, got %d", len(files))
	}
}

func TestCorpus_Save_DifferentContent_CreatesSeparateFiles(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	c.Save(sampleInput(`{"amount":1}`))
	c.Save(sampleInput(`{"amount":2}`))
	files, _ := os.ReadDir(dir)
	if len(files) != 2 {
		t.Errorf("expected 2 files for different content, got %d", len(files))
	}
}

func TestCorpus_Save_ProducesValidJSON(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	c.Save(sampleInput(`{"amount":5}`))
	files, _ := os.ReadDir(dir)
	data, _ := os.ReadFile(filepath.Join(dir, files[0].Name()))
	var decoded mutate.Input
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("saved corpus file is not valid JSON: %v", err)
	}
}

func TestCorpus_Save_RoundTrip_PreservesBody(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	original := sampleInput(`{"amount":77,"currency":"EUR"}`)
	c.Save(original)
	entries, _ := c.Load()
	if len(entries) == 0 {
		t.Fatal("no entries after Save+Load")
	}
	if string(entries[0].Body) != string(original.Body) {
		t.Errorf("body mismatch: got %s, want %s", entries[0].Body, original.Body)
	}
}

// --- Size ---

func TestCorpus_Size_EmptyDir_IsZero(t *testing.T) {
	c := corpus.NewCorpus(t.TempDir())
	c.Load()
	if c.Size() != 0 {
		t.Errorf("expected size 0 on empty corpus, got %d", c.Size())
	}
}

func TestCorpus_Size_AfterLoad_ReflectsEntryCount(t *testing.T) {
	dir := t.TempDir()
	c := corpus.NewCorpus(dir)
	c.Save(sampleInput(`{"amount":1}`))
	c.Save(sampleInput(`{"amount":2}`))
	c.Load()
	if c.Size() != 2 {
		t.Errorf("expected size 2 after loading 2 entries, got %d", c.Size())
	}
}
```

---

## Step 4 — Failure classifier

### Spec

The classifier lives at `internal/strategy/fuzz/classify`. It takes a raw HTTP response and the error returned by the runner, and produces a `Classification`.

- `Classification` is a string enum:
  - `"crash"` — the server returned no response (connection refused, reset, EOF) or panicked (detected via a 500 with a stack trace in the body)
  - `"timeout"` — the request exceeded the enforcer's `RequestTimeout`
  - `"validation_leak"` — the response body contains internal implementation details: stack traces, file paths (`/home/`, `/var/`, `C:\`), Go error strings (`goroutine`, `runtime error`), database error messages (`SQL`, `pq:`, `mysql:`)
  - `"schema_break"` — the response body does not conform to the declared response schema for the returned status code (uses the same `validate.ValidateResponse` logic from M4)
  - `"unexpected_status"` — the response status is not in the set of status codes declared in the OpenAPI spec for the operation (e.g. a 422 from an endpoint that only declares 200 and 400)
  - `"pass"` — none of the above conditions are met
- `Classify(resp *http.Response, body []byte, err error, op model.Operation) Classification` — applies all classifiers in priority order: crash > timeout > validation_leak > schema_break > unexpected_status > pass
- Classification is deterministic — same inputs always produce the same output
- `Classify` must never panic, even when `resp` is nil or `body` is empty

### Tests

`internal/strategy/fuzz/classify/classify_test.go`:

```go
package classify_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/strategy/fuzz/classify"
	"github.com/jimbery/bt/pkg/model"
)

func opWithStatuses(codes ...int) model.Operation {
	schemas := map[int]model.Schema{}
	for _, c := range codes {
		schemas[c] = model.Schema{Type: "object"}
	}
	return model.Operation{ResponseSchemas: schemas}
}

func resp(code int) *http.Response {
	return &http.Response{StatusCode: code}
}

// --- crash ---

func TestClassify_NilResponse_IsCrash(t *testing.T) {
	got := classify.Classify(nil, nil, errors.New("connection refused"), opWithStatuses(200))
	if got != classify.ClassificationCrash {
		t.Errorf("expected crash for nil response, got %q", got)
	}
}

func TestClassify_NetworkError_IsCrash(t *testing.T) {
	got := classify.Classify(nil, nil, errors.New("EOF"), opWithStatuses(200))
	if got != classify.ClassificationCrash {
		t.Errorf("expected crash for network error, got %q", got)
	}
}

func TestClassify_500WithStackTrace_IsCrash(t *testing.T) {
	body := []byte("goroutine 1 [running]:\nruntime error: index out of range")
	got := classify.Classify(resp(500), body, nil, opWithStatuses(200, 500))
	if got != classify.ClassificationCrash {
		t.Errorf("expected crash for 500 with stack trace, got %q", got)
	}
}

// --- timeout ---

func TestClassify_TimeoutError_IsTimeout(t *testing.T) {
	got := classify.Classify(nil, nil, classify.ErrTimeout, opWithStatuses(200))
	if got != classify.ClassificationTimeout {
		t.Errorf("expected timeout, got %q", got)
	}
}

// --- validation_leak ---

func TestClassify_BodyContainsGoroutine_IsValidationLeak(t *testing.T) {
	body := []byte(`{"error":"goroutine 5 [running]: main.handler"}`)
	got := classify.Classify(resp(500), body, nil, opWithStatuses(200, 500))
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak for goroutine in body, got %q", got)
	}
}

func TestClassify_BodyContainsFilePath_IsValidationLeak(t *testing.T) {
	body := []byte(`{"error":"open /var/app/config.yaml: no such file"}`)
	got := classify.Classify(resp(500), body, nil, opWithStatuses(200, 500))
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak for file path in body, got %q", got)
	}
}

func TestClassify_BodyContainsSQLError_IsValidationLeak(t *testing.T) {
	body := []byte(`{"error":"pq: duplicate key value violates unique constraint"}`)
	got := classify.Classify(resp(400), body, nil, opWithStatuses(200, 400))
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak for SQL error in body, got %q", got)
	}
}

func TestClassify_BodyContainsWindowsPath_IsValidationLeak(t *testing.T) {
	body := []byte(`{"error":"open C:\\Users\\app\\config: access denied"}`)
	got := classify.Classify(resp(500), body, nil, opWithStatuses(200, 500))
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak for Windows path in body, got %q", got)
	}
}

// --- schema_break ---

func TestClassify_ResponseViolatesSchema_IsSchemaBreak(t *testing.T) {
	schema := model.Schema{
		Type:     "object",
		Required: []string{"id", "status"},
		Properties: map[string]model.Schema{
			"id":     {Type: "string"},
			"status": {Type: "string"},
		},
	}
	op := model.Operation{
		ResponseSchemas: map[int]model.Schema{200: schema},
	}
	// Body missing required 'status' field.
	body := []byte(`{"id":"ord-001"}`)
	got := classify.Classify(resp(200), body, nil, op)
	if got != classify.ClassificationSchemaBreak {
		t.Errorf("expected schema_break for missing required field, got %q", got)
	}
}

func TestClassify_ResponseMatchesSchema_IsNotSchemaBreak(t *testing.T) {
	schema := model.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]model.Schema{
			"id": {Type: "string"},
		},
	}
	op := model.Operation{
		ResponseSchemas: map[int]model.Schema{200: schema},
	}
	body := []byte(`{"id":"ord-001"}`)
	got := classify.Classify(resp(200), body, nil, op)
	if got != classify.ClassificationPass {
		t.Errorf("expected pass for valid body, got %q", got)
	}
}

// --- unexpected_status ---

func TestClassify_UndeclaredStatusCode_IsUnexpectedStatus(t *testing.T) {
	// Op only declares 200 and 400. A 422 is unexpected.
	got := classify.Classify(resp(422), []byte(`{"error":"unprocessable"}`), nil, opWithStatuses(200, 400))
	if got != classify.ClassificationUnexpectedStatus {
		t.Errorf("expected unexpected_status for 422, got %q", got)
	}
}

func TestClassify_DeclaredStatusCode_IsNotUnexpectedStatus(t *testing.T) {
	got := classify.Classify(resp(400), []byte(`{"error":"bad request","code":"BAD"}`), nil, opWithStatuses(200, 400))
	// Should be pass (assuming clean body and matching schema).
	if got == classify.ClassificationUnexpectedStatus {
		t.Error("expected not unexpected_status for a declared 400, got unexpected_status")
	}
}

// --- pass ---

func TestClassify_CleanResponse_IsPass(t *testing.T) {
	schema := model.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]model.Schema{
			"id": {Type: "string"},
		},
	}
	op := model.Operation{
		ResponseSchemas: map[int]model.Schema{200: schema},
	}
	body := []byte(`{"id":"ord-001"}`)
	got := classify.Classify(resp(200), body, nil, op)
	if got != classify.ClassificationPass {
		t.Errorf("expected pass for clean response, got %q", got)
	}
}

// --- priority order ---

func TestClassify_CrashTakesPriorityOverValidationLeak(t *testing.T) {
	// Nil response (crash) + body with goroutine text (leak) — crash wins.
	body := []byte("goroutine 1 [running]:")
	got := classify.Classify(nil, body, errors.New("EOF"), opWithStatuses(200))
	if got != classify.ClassificationCrash {
		t.Errorf("expected crash to take priority over validation_leak, got %q", got)
	}
}

func TestClassify_ValidationLeakTakesPriorityOverSchemaBreak(t *testing.T) {
	schema := model.Schema{
		Type:     "object",
		Required: []string{"id"},
		Properties: map[string]model.Schema{
			"id": {Type: "string"},
		},
	}
	op := model.Operation{
		ResponseSchemas: map[int]model.Schema{500: schema},
	}
	// Body has a goroutine (leak) AND is missing 'id' (schema break).
	body := []byte(`{"error":"goroutine 1 [running]"}`)
	got := classify.Classify(resp(500), body, nil, op)
	if got != classify.ClassificationValidationLeak {
		t.Errorf("expected validation_leak to take priority over schema_break, got %q", got)
	}
}

// --- nil safety ---

func TestClassify_NilBody_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Classify panicked on nil body: %v", r)
		}
	}()
	classify.Classify(resp(200), nil, nil, opWithStatuses(200))
}

func TestClassify_EmptyBody_DoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Classify panicked on empty body: %v", r)
		}
	}()
	classify.Classify(resp(200), []byte{}, nil, opWithStatuses(200))
}
```

---

## Step 5 — Fuzz runner

### Spec

The fuzz runner lives at `internal/strategy/fuzz`. It implements `engine.Strategy` and wires together the safety model, corpus, mutators, and classifier.

- `Runner` implements `engine.Strategy`
- `Runner.Run(ctx context.Context, plan model.TestPlan) ([]model.Result, error)`
- For each operation in the plan, in order:
  1. Check the safety enforcer: if the operation's method is blocked, skip the operation and emit a `model.Result` with `Skipped: true` and `SkipReason: "method blocked by safety profile"`
  2. Load the corpus; fall back to a built-in seed corpus if the corpus directory is empty or unconfigured
  3. For each corpus entry, apply `MutatorSet.MutateAll` to produce a set of variants
  4. For each variant, respect the throttle delay from the enforcer before sending the request
  5. Execute the HTTP request via the existing runner with the enforcer's timeout
  6. Classify the response using `classify.Classify`
  7. On any non-`pass` classification, write an artifact and record a `model.Failure`; save the input to the corpus as an interesting case
  8. On `pass`, do not write an artifact; the input is not saved to corpus (it is not interesting)
  9. Honour context cancellation between requests — do not start a new request if `ctx.Done()` is closed
- The total number of mutations per operation is bounded by `RunConfig.FuzzIterations` (default: 50 in safe mode)
- The runner logs the number of mutations sent and failures found per operation at `INFO` level
- Every `model.Result` produced by the fuzz runner has `StrategyKind: "fuzz"` and includes `Classification` in the result

### Tests

`internal/strategy/fuzz/runner_test.go`:

```go
package fuzz_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jimbery/bt/internal/strategy/fuzz"
	"github.com/jimbery/bt/internal/strategy/fuzz/safety"
	"github.com/jimbery/bt/pkg/model"
)

func safePlan(baseURL string, ops ...model.Operation) model.TestPlan {
	return model.TestPlan{
		Target:     model.Target{BaseURL: baseURL},
		Operations: ops,
		RunConfig: model.RunConfig{
			FuzzIterations: 10, // keep tests fast
			Safety: model.SafetyConfig{
				Profile: "safe",
			},
		},
	}
}

func getOp(id string) model.Operation {
	return model.Operation{
		ID:     id,
		Method: "GET",
		Path:   "/orders/ord-001",
		ResponseSchemas: map[int]model.Schema{
			200: {
				Type:     "object",
				Required: []string{"id"},
				Properties: map[string]model.Schema{"id": {Type: "string"}},
			},
		},
	}
}

func postOp(id string) model.Operation {
	return model.Operation{
		ID:     id,
		Method: "POST",
		Path:   "/orders",
		ResponseSchemas: map[int]model.Schema{
			201: {
				Type:     "object",
				Required: []string{"id"},
				Properties: map[string]model.Schema{"id": {Type: "string"}},
			},
		},
	}
}

// --- Safety enforcement ---

func TestRunner_BlockedMethod_SkipsOperation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("blocked method should never reach the server")
	}))
	defer srv.Close()

	deleteOp := model.Operation{
		ID:     "DeleteOrder",
		Method: "DELETE",
		Path:   "/orders/ord-001",
	}
	plan := safePlan(srv.URL, deleteOp)

	runner := fuzz.NewRunner(t.TempDir())
	results, err := runner.Run(context.Background(), plan)
	if err != nil {
		t.Fatalf("Run returned unexpected error: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for a skipped operation")
	}
	if !results[0].Skipped {
		t.Errorf("expected Skipped=true for DELETE under safe profile, got false")
	}
	if results[0].SkipReason == "" {
		t.Error("expected SkipReason to be set for skipped operation")
	}
}

func TestRunner_AllowedMethod_ReachesServer(t *testing.T) {
	reached := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"ord-001"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(t.TempDir())
	runner.Run(context.Background(), plan)

	if !reached {
		t.Error("GET (allowed by safe profile) should have reached the server")
	}
}

// --- Classification in results ---

func TestRunner_CrashResponse_ClassifiedAsCrash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate a crash: drop the connection.
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "crash" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected at least one crash classification for a dropped connection")
	}
}

func TestRunner_ValidationLeak_ClassifiedCorrectly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"goroutine 1 [running]: main.handler panic"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "validation_leak" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected validation_leak classification for body containing goroutine trace")
	}
}

func TestRunner_SchemaBreak_ClassifiedCorrectly(t *testing.T) {
	// Server returns body missing required 'id' field.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"status":"ok"}`)) // missing required 'id'
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "schema_break" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected schema_break classification for response missing required field")
	}
}

func TestRunner_UnexpectedStatus_ClassifiedCorrectly(t *testing.T) {
	// Server returns 418, which is not declared in the operation's response schemas.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(418)
		w.Write([]byte(`{"error":"I am a teapot"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	found := false
	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "unexpected_status" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected unexpected_status for 418 response from op declaring only 200")
	}
}

// --- Artifact production ---

func TestRunner_NonPassClassification_WritesArtifact(t *testing.T) {
	artifactDir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"always fails"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(artifactDir)
	runner.Run(context.Background(), plan)

	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		t.Fatalf("cannot read artifact dir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one artifact for a non-pass classification")
	}
}

func TestRunner_PassClassification_NoArtifactWritten(t *testing.T) {
	artifactDir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"ord-001"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(artifactDir)
	runner.Run(context.Background(), plan)

	entries, _ := os.ReadDir(artifactDir)
	if len(entries) != 0 {
		t.Errorf("expected no artifacts for passing fuzz run, got %d", len(entries))
	}
}

// --- Result shape ---

func TestRunner_Results_HaveStrategyKindFuzz(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"ord-001"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	for _, r := range results {
		if r.StrategyKind != "fuzz" {
			t.Errorf("expected StrategyKind='fuzz', got %q", r.StrategyKind)
		}
	}
}

func TestRunner_Failure_ClassificationFieldIsSet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	plan := safePlan(srv.URL, getOp("GetOrder"))
	runner := fuzz.NewRunner(t.TempDir())
	results, _ := runner.Run(context.Background(), plan)

	for _, r := range results {
		for _, f := range r.Failures {
			if f.Classification == "" {
				t.Error("every fuzz failure must have a Classification set")
			}
		}
	}
}

// --- Context cancellation ---

func TestRunner_ContextCancellation_StopsCleanly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"ord-001"}`))
	}))
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{getOp("GetOrder")},
		RunConfig: model.RunConfig{
			FuzzIterations: 1000,
			Safety:         model.SafetyConfig{Profile: "safe"},
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	runner := fuzz.NewRunner(t.TempDir())
	_, err := runner.Run(ctx, plan)
	_ = err // should not hang
}

// --- Corpus saving ---

func TestRunner_InterestingInput_SavedToCorpus(t *testing.T) {
	corpusDir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"always fails"}`))
	}))
	defer srv.Close()

	plan := model.TestPlan{
		Target:     model.Target{BaseURL: srv.URL},
		Operations: []model.Operation{getOp("GetOrder")},
		RunConfig: model.RunConfig{
			FuzzIterations: 5,
			CorpusDir:      corpusDir,
			Safety:         model.SafetyConfig{Profile: "safe"},
		},
	}

	runner := fuzz.NewRunner(t.TempDir())
	runner.Run(context.Background(), plan)

	entries, _ := os.ReadDir(corpusDir)
	if len(entries) == 0 {
		t.Error("expected at least one interesting input to be saved to corpus on failure")
	}
}
```

---

## Step 6 — Reporter updates

### Spec

The console reporter is updated to render fuzz results with richer classification detail:

- Format for a fuzz failure:
  ```
    FAIL  GetOrder [fuzz]           10 mutations  (3 failures)
         crash:             connection dropped — no response received
           input:           GET /orders/ord-001%00
           artifact:        .bt/artifacts/2026-05-09T160000Z-GetOrder-crash.json

         validation_leak:   body contains internal path: /var/app/config.yaml
           input:           GET /orders/' OR 1=1--
           artifact:        .bt/artifacts/2026-05-09T160000Z-GetOrder-leak.json

         schema_break:      $.id: missing required field
           input:           GET /orders/<script>alert(1)</script>
           artifact:        .bt/artifacts/2026-05-09T160000Z-GetOrder-schema.json

    SKIP  DeleteOrder [fuzz]        method blocked by safety profile (safe)
  ```
- Skipped operations are rendered with `SKIP` and the skip reason
- The summary line counts both passed operations and skipped operations separately: `3 operations tested: 1 passed, 1 failed, 1 skipped`

### Tests

`internal/report/fuzz_reporter_test.go`:

```go
package report_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/report"
	"github.com/jimbery/bt/pkg/model"
)

func fuzzFailureResult() model.Result {
	return model.Result{
		CaseID:       "GetOrder",
		StrategyKind: "fuzz",
		MutationCount: 10,
		Failures: []model.Failure{
			{
				Classification: "crash",
				Message:        "connection dropped — no response received",
				MutatedInput:   "GET /orders/ord-001%00",
				ArtifactPath:   ".bt/artifacts/2026-05-09T160000Z-GetOrder-crash.json",
			},
			{
				Classification: "validation_leak",
				Message:        "body contains internal path: /var/app/config.yaml",
				MutatedInput:   "GET /orders/' OR 1=1--",
				ArtifactPath:   ".bt/artifacts/2026-05-09T160000Z-GetOrder-leak.json",
			},
		},
	}
}

func fuzzSkippedResult() model.Result {
	return model.Result{
		CaseID:       "DeleteOrder",
		StrategyKind: "fuzz",
		Skipped:      true,
		SkipReason:   "method blocked by safety profile (safe)",
	}
}

func TestFuzzReporter_Failure_PrintsFAIL(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), "FAIL") {
		t.Error("expected 'FAIL' in fuzz failure output")
	}
}

func TestFuzzReporter_Failure_PrintsStrategyKind(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), "fuzz") {
		t.Error("expected 'fuzz' strategy kind in output")
	}
}

func TestFuzzReporter_Failure_PrintsClassification(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	output := buf.String()
	if !strings.Contains(output, "crash") {
		t.Error("expected 'crash' classification in output")
	}
	if !strings.Contains(output, "validation_leak") {
		t.Error("expected 'validation_leak' classification in output")
	}
}

func TestFuzzReporter_Failure_PrintsMutatedInput(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), "/orders/ord-001%00") {
		t.Error("expected mutated input path in output")
	}
}

func TestFuzzReporter_Failure_PrintsArtifactPath(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), ".bt/artifacts/") {
		t.Error("expected artifact path in fuzz failure output")
	}
}

func TestFuzzReporter_Failure_PrintsMutationCount(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzFailureResult()})
	if !strings.Contains(buf.String(), "10") {
		t.Error("expected mutation count '10' in output")
	}
}

func TestFuzzReporter_Skipped_PrintsSKIP(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzSkippedResult()})
	if !strings.Contains(buf.String(), "SKIP") {
		t.Error("expected 'SKIP' for skipped operation")
	}
}

func TestFuzzReporter_Skipped_PrintsSkipReason(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{fuzzSkippedResult()})
	if !strings.Contains(buf.String(), "method blocked by safety profile") {
		t.Error("expected skip reason in output")
	}
}

func TestFuzzReporter_Summary_CountsSkippedSeparately(t *testing.T) {
	buf := &bytes.Buffer{}
	r := report.NewConsoleReporter(buf)
	r.Render([]model.Result{
		fuzzFailureResult(),
		fuzzSkippedResult(),
		{CaseID: "GetHealth", StrategyKind: "fuzz", Failures: nil},
	})
	output := buf.String()
	if !strings.Contains(output, "skipped") {
		t.Error("expected 'skipped' in summary line")
	}
}
```

---

## Step 7 — CLI integration

### Spec

- `bt run --strategy fuzz` runs the fuzz strategy
- `--safety <profile>` flag selects the safety profile (default: `safe`); must be one of `safe`, `aggressive`, `destructive`
- `--safety destructive` is allowed as a CLI flag but sets `WithDestructiveConfirmed(true)` internally — it cannot be expressed in a config file alone
- `--fuzz-iterations <n>` controls the number of mutation iterations per operation (default: 50)
- `--corpus-dir <path>` sets the corpus directory (default: `<config dir>/corpus/`)
- All flags appear in `bt run --help`

### Tests

`internal/cli/run_fuzz_flags_test.go`:

```go
package cli_test

import (
	"bytes"
	"testing"

	"github.com/jimbery/bt/internal/cli"
)

func TestRunCommand_FuzzFlags_SafetyParsed(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--strategy", "fuzz", "--safety", "safe", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error parsing --safety flag: %v", err)
	}
}

func TestRunCommand_FuzzFlags_IterationsParsed(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--strategy", "fuzz", "--fuzz-iterations", "200", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error parsing --fuzz-iterations flag: %v", err)
	}
}

func TestRunCommand_FuzzFlags_CorpusDirParsed(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--strategy", "fuzz", "--corpus-dir", "/tmp/corpus", "--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error parsing --corpus-dir flag: %v", err)
	}
}

func TestRunCommand_FuzzFlags_AllAppearInHelp(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--help"})
	cmd.Execute()
	output := buf.String()
	for _, flag := range []string{"--safety", "--fuzz-iterations", "--corpus-dir"} {
		if !bytes.Contains([]byte(output), []byte(flag)) {
			t.Errorf("expected flag %q in 'bt run --help' output", flag)
		}
	}
}

func TestRunCommand_FuzzFlags_InvalidSafetyProfile_ReturnsError(t *testing.T) {
	cmd := cli.NewRootCmd()
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"run", "--strategy", "fuzz", "--safety", "yolo"})
	err := cmd.Execute()
	if err == nil {
		t.Error("expected error for invalid --safety profile value")
	}
}
```

---

## Local verification

```bash
# Unit tests — all fuzz strategy packages
go test ./internal/strategy/fuzz/... -race -v
go test ./internal/strategy/fuzz/safety/... -race -v
go test ./internal/strategy/fuzz/mutate/... -race -v
go test ./internal/strategy/fuzz/corpus/... -race -v
go test ./internal/strategy/fuzz/classify/... -race -v
go test ./internal/report/... -race -v
go test ./internal/cli/... -race -v

# Build and smoke test (safe profile — safe to run against orders API)
go build -o bt ./cmd/bt
go run ./examples/orders-api &
sleep 1

./bt run \
  --config examples/orders-api/bt/backendtest.yaml \
  --strategy fuzz \
  --safety safe \
  --fuzz-iterations 20

kill %1
```

---

## Model additions required

The following fields must be added to the domain model before implementation begins. These must be confirmed against the current state of `pkg/model/model.go` before writing any code:

| Type | Field | Type | Purpose |
|---|---|---|---|
| `model.Result` | `Skipped` | `bool` | Operation was skipped by safety enforcer |
| `model.Result` | `SkipReason` | `string` | Human-readable reason for skip |
| `model.Result` | `MutationCount` | `int` | Number of mutations sent for this operation |
| `model.Failure` | `Classification` | `string` | Fuzz failure classification (crash, timeout, etc.) |
| `model.Failure` | `MutatedInput` | `string` | Human-readable representation of the mutated input |
| `model.RunConfig` | `FuzzIterations` | `int` | Max mutations per operation |
| `model.RunConfig` | `CorpusDir` | `string` | Path to corpus directory |
| `model.RunConfig` | `Safety` | `model.SafetyConfig` | Inlined safety config |
| `model.SafetyConfig` | `Profile` | `string` | Safety profile name |
| `model.SafetyConfig` | `AllowedMethods` | `[]string` | Method allow list override |
| `model.SafetyConfig` | `DeniedMethods` | `[]string` | Method deny list override |
| `model.SafetyConfig` | `MaxRequestsPerSecond` | `float64` | Throttle rate |
| `model.SafetyConfig` | `TimeoutSeconds` | `float64` | Per-request timeout |

---

## M5 exit criterion

`bt run --strategy fuzz --safety safe` runs in CI without manual guards and produces classified failure reports. Specifically:

- DELETE and PUT are blocked by the safe profile without any per-operation configuration
- Every non-pass result carries a `Classification` field with one of: `crash`, `timeout`, `validation_leak`, `schema_break`, `unexpected_status`
- Every non-pass result has an artifact bundle written to `.bt/artifacts/`
- Interesting inputs (those that produced failures) are saved to the corpus for reuse
- Passing runs produce no artifacts and do not grow the corpus
- The `--safety destructive` flag cannot be activated via config file — it requires an explicit CLI flag
- All unit tests pass with `-race`