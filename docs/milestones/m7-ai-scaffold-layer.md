# M7 — AI Scaffold Layer

This document follows the same structure as M1–M6: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist. Tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

---

## Overview

M7 adds AI-backed reasoning to the scaffold tools built in M6. The execution engine is untouched — AI never runs during `bt run`, never evaluates invariants, and never writes to artifacts. It only operates on schemas, operations, and configs, and only in response to explicit tool calls.

The three pieces built here are:

1. **`AIProvider` interface and implementations** — the abstraction that separates the AI call from the tools that use it; a stub implementation for no-key environments, and one real implementation (Anthropic Claude)
2. **Upgraded `bt_suggest_strategy`** — replaces M6's rule-based logic with an AI-backed path that reasons over the full operation schema, while keeping the rule-based fallback for when no provider is configured
3. **`bt_suggest_invariants`** — the new M7 tool; given a single operation, returns structured invariant candidates with rationale and confidence for human review

Each piece has its own spec, tests, and implementation section. Build and verify in order.

**Exit criterion:** Pointing an MCP client at the orders API schema produces a set of invariant suggestions for the create endpoint that includes at least `no_5xx`, `response_matches_schema`, and one domain-specific candidate. The tool runs and returns a valid response when no API key is configured, using the stub provider. All unit tests pass with `-race`.

---

## Step 1 — `AIProvider` interface

### Spec

The provider interface lives at `internal/ai/`. It is the only place in the codebase that knows anything about a specific AI service. Nothing outside this package imports an AI SDK directly.

- `Provider` interface:
  ```go
  type Provider interface {
      // Complete sends a prompt and returns the model's response text.
      // The prompt is plain text; structured output is requested via
      // prompt content, not a separate API parameter.
      Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
  }
  ```
- `CompletionRequest`:
  ```go
  type CompletionRequest struct {
      SystemPrompt string // describes the model's role and output format
      UserPrompt   string // the specific question or schema to reason over
      MaxTokens    int    // caller sets an upper bound; provider must not exceed it
  }
  ```
- `CompletionResponse`:
  ```go
  type CompletionResponse struct {
      Text         string // raw model output; caller is responsible for parsing
      InputTokens  int    // tokens consumed by the prompt (for logging)
      OutputTokens int    // tokens in the response (for logging)
  }
  ```
- `StubProvider` — returns a hardcoded canned response; used when no API key is configured and in all unit tests. The canned response is valid JSON matching the schema the caller expects.
- `AnthropicProvider` — calls the Anthropic Messages API using `claude-sonnet-4-5` (or whichever model is configured). API key is read from `ANTHROPIC_API_KEY` environment variable or `~/.config/bt/config.yaml`.
- `NewProvider(cfg ProviderConfig) (Provider, error)` — returns `StubProvider` when `cfg.APIKey` is empty; returns `AnthropicProvider` when a key is present. Never returns nil without an error.
- `ProviderConfig`:
  ```go
  type ProviderConfig struct {
      APIKey   string
      Model    string // defaults to "claude-sonnet-4-5" if empty
      MaxTokens int   // defaults to 1024 if zero
  }
  ```
- Both providers honour context cancellation — if `ctx` is cancelled before the response arrives, `Complete` returns `ctx.Err()`
- `AnthropicProvider` retries once on HTTP 529 (overloaded) with a 2-second backoff; all other errors are returned immediately
- The provider package must not import anything from `internal/engine`, `internal/strategy`, `internal/mcp`, or `internal/cli`

### Tests

`internal/ai/provider_test.go`:

```go
package ai_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jimbery/bt/internal/ai"
)

// --- NewProvider ---

func TestNewProvider_EmptyAPIKey_ReturnsStub(t *testing.T) {
	p, err := ai.NewProvider(ai.ProviderConfig{APIKey: ""})
	if err != nil {
		t.Fatalf("unexpected error with empty key: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	// Confirm it behaves like a stub: completes without network access.
	resp, err := p.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "respond with: OK",
		UserPrompt:   "test",
		MaxTokens:    10,
	})
	if err != nil {
		t.Fatalf("stub should not error: %v", err)
	}
	if resp.Text == "" {
		t.Error("stub should return non-empty text")
	}
}

func TestNewProvider_EmptyModel_UsesDefault(t *testing.T) {
	p, err := ai.NewProvider(ai.ProviderConfig{APIKey: "test-key", Model: ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewProvider_ZeroMaxTokens_UsesDefault(t *testing.T) {
	p, err := ai.NewProvider(ai.ProviderConfig{APIKey: "", MaxTokens: 0})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
}

// --- StubProvider ---

func TestStubProvider_Complete_ReturnsNonEmptyText(t *testing.T) {
	stub := ai.NewStubProvider("test response")
	resp, err := stub.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text == "" {
		t.Error("stub must return non-empty text")
	}
}

func TestStubProvider_Complete_ReturnsConfiguredText(t *testing.T) {
	expected := `{"suggestions":[]}`
	stub := ai.NewStubProvider(expected)
	resp, err := stub.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Text != expected {
		t.Errorf("expected %q, got %q", expected, resp.Text)
	}
}

func TestStubProvider_Complete_HonoursContextCancellation(t *testing.T) {
	stub := ai.NewStubProvider("text")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := stub.Complete(ctx, ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    10,
	})
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestStubProvider_Complete_TokenCountsAreNonNegative(t *testing.T) {
	stub := ai.NewStubProvider("response text")
	resp, _ := stub.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "user",
		MaxTokens:    100,
	})
	if resp.InputTokens < 0 {
		t.Errorf("InputTokens must be >= 0, got %d", resp.InputTokens)
	}
	if resp.OutputTokens < 0 {
		t.Errorf("OutputTokens must be >= 0, got %d", resp.OutputTokens)
	}
}

// --- CompletionRequest validation ---

func TestCompletionRequest_EmptyUserPrompt_StubStillResponds(t *testing.T) {
	stub := ai.NewStubProvider("ok")
	_, err := stub.Complete(context.Background(), ai.CompletionRequest{
		SystemPrompt: "system",
		UserPrompt:   "",
		MaxTokens:    100,
	})
	// Stub must not error on empty prompt.
	if err != nil {
		t.Fatalf("unexpected error for empty user prompt: %v", err)
	}
}

// --- ProviderConfig ---

func TestProviderConfig_DefaultModel_IsSet(t *testing.T) {
	cfg := ai.ProviderConfig{APIKey: "key"}
	p, _ := ai.NewProvider(cfg)
	// We can't inspect the model directly without reflection,
	// but we can confirm it doesn't panic or error on Complete.
	_, err := p.Complete(context.Background(), ai.CompletionRequest{
		UserPrompt: "test",
		MaxTokens:  10,
	})
	// AnthropicProvider will error (no real API key), but should not panic.
	// StubProvider should succeed. Either is acceptable here.
	_ = err
}
```

---

## Step 2 — Prompt builder

### Spec

The prompt builder lives at `internal/ai/prompt/`. It constructs the system and user prompts for each AI-backed tool call. Prompts are tested separately from the provider so the logic can be verified without making API calls.

- `InvariantSuggestionsPrompt(op model.Operation) CompletionRequest` — builds the prompt for `bt_suggest_invariants`
- `StrategyRecommendationPrompt(ops []model.Operation) CompletionRequest` — builds the prompt for the AI-backed `bt_suggest_strategy`
- Both functions return a `CompletionRequest` with a populated `SystemPrompt`, `UserPrompt`, and `MaxTokens`

**System prompt for invariant suggestions** (exact wording is implementation detail, but must instruct the model to):
- Respond only in JSON — no preamble, no markdown fences
- Return an array of suggestion objects, each with: `name` (string), `rationale` (string, ≥20 chars), `confidence` (`"high"`, `"medium"`, or `"low"`), `invariant_type` (one of: `"no_5xx"`, `"response_matches_schema"`, `"idempotency"`, `"custom"`), and optionally `config` (a map of string to any, for custom invariants that need parameters)
- Always suggest `no_5xx` and `response_matches_schema` for any operation
- Suggest `idempotency` for POST operations with a body
- Suggest domain-specific `custom` invariants based on the operation's schema — e.g. if the response has an `amount` field of type `number`, suggest an invariant that checks the returned amount matches the submitted amount

**System prompt for strategy recommendations** (must instruct the model to):
- Respond only in JSON — no preamble, no markdown fences
- Return an array of recommendation objects matching the M6 output schema (`operation_id`, `strategies` array with `strategy`, `priority`, `rationale`)
- Provide specific rationale referencing the operation's schema characteristics (e.g. "this operation accepts a `currency` enum field, making it a good candidate for property testing to verify enum boundary behaviour")

**User prompt construction:**
- For invariant suggestions: serialises `model.Operation` as JSON, including method, path, parameter names/types, request body schema, and response schemas — but caps at 4000 characters to avoid context overflow; if the schema is larger, includes only required fields and top-level properties
- For strategy recommendations: serialises the operation list as a compact JSON array

- Both prompts must be deterministic for a given input — same operation always produces the same prompt text
- Neither function makes any network calls

### Tests

`internal/ai/prompt/prompt_test.go`:

```go
package prompt_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/ai/prompt"
	"github.com/jimbery/bt/pkg/model"
)

// --- Helpers ---

func createOrderOp() model.Operation {
	return model.Operation{
		ID:     "CreateOrder",
		Method: "POST",
		Path:   "/orders",
		RequestBodySchema: &model.Schema{
			Type: "object",
			Properties: map[string]model.Schema{
				"amount":      {Type: "number", Minimum: func() *float64 { v := float64(0.01); return &v }()},
				"currency":    {Type: "string", Enum: []any{"GBP", "USD", "EUR"}},
				"description": {Type: "string", Nullable: true},
			},
			Required: []string{"amount", "currency"},
		},
		ResponseSchemas: map[int]model.Schema{
			201: {
				Type: "object",
				Properties: map[string]model.Schema{
					"id":       {Type: "string"},
					"amount":   {Type: "number"},
					"currency": {Type: "string"},
					"status":   {Type: "string", Enum: []any{"pending", "complete", "cancelled"}},
				},
				Required: []string{"id", "amount", "currency", "status"},
			},
			400: {
				Type:       "object",
				Properties: map[string]model.Schema{"error": {Type: "string"}, "code": {Type: "string"}},
				Required:   []string{"error", "code"},
			},
		},
	}
}

func getHealthOp() model.Operation {
	return model.Operation{
		ID:     "GetHealth",
		Method: "GET",
		Path:   "/health",
		ResponseSchemas: map[int]model.Schema{
			200: {Type: "object", Properties: map[string]model.Schema{"status": {Type: "string"}}, Required: []string{"status"}},
		},
	}
}

// --- InvariantSuggestionsPrompt ---

func TestInvariantSuggestionsPrompt_SystemPrompt_IsNonEmpty(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	if req.SystemPrompt == "" {
		t.Error("SystemPrompt must not be empty")
	}
}

func TestInvariantSuggestionsPrompt_SystemPrompt_InstructsJSONOnly(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	lower := strings.ToLower(req.SystemPrompt)
	if !strings.Contains(lower, "json") {
		t.Error("SystemPrompt must instruct the model to respond in JSON")
	}
}

func TestInvariantSuggestionsPrompt_SystemPrompt_InstructsNoPreamble(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	lower := strings.ToLower(req.SystemPrompt)
	// Must mention no preamble/markdown/backticks.
	hasNoMarkdown := strings.Contains(lower, "no preamble") ||
		strings.Contains(lower, "no markdown") ||
		strings.Contains(lower, "backtick") ||
		strings.Contains(lower, "only json") ||
		strings.Contains(lower, "only valid json")
	if !hasNoMarkdown {
		t.Error("SystemPrompt must explicitly prohibit preamble or markdown fences")
	}
}

func TestInvariantSuggestionsPrompt_UserPrompt_ContainsOperationID(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	if !strings.Contains(req.UserPrompt, "CreateOrder") {
		t.Error("UserPrompt must include the operation ID")
	}
}

func TestInvariantSuggestionsPrompt_UserPrompt_ContainsMethod(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	if !strings.Contains(req.UserPrompt, "POST") {
		t.Error("UserPrompt must include the HTTP method")
	}
}

func TestInvariantSuggestionsPrompt_UserPrompt_ContainsPath(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	if !strings.Contains(req.UserPrompt, "/orders") {
		t.Error("UserPrompt must include the path")
	}
}

func TestInvariantSuggestionsPrompt_UserPrompt_ContainsSchemaInfo(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	// At minimum the request body field names should appear.
	if !strings.Contains(req.UserPrompt, "amount") {
		t.Error("UserPrompt must include request body field names")
	}
}

func TestInvariantSuggestionsPrompt_UserPrompt_IsValidJSON_WhenSchemaIncluded(t *testing.T) {
	// The user prompt wraps the operation as JSON — confirm it's parseable.
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	// Extract the JSON portion (everything after the first '{').
	idx := strings.Index(req.UserPrompt, "{")
	if idx == -1 {
		t.Skip("UserPrompt does not contain a JSON block — acceptable if schema is embedded differently")
	}
	jsonPart := req.UserPrompt[idx:]
	var v map[string]any
	if err := json.Unmarshal([]byte(jsonPart), &v); err != nil {
		t.Errorf("JSON portion of UserPrompt is not valid JSON: %v\nraw: %s", err, jsonPart)
	}
}

func TestInvariantSuggestionsPrompt_MaxTokens_IsPositive(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	if req.MaxTokens <= 0 {
		t.Errorf("MaxTokens must be positive, got %d", req.MaxTokens)
	}
}

func TestInvariantSuggestionsPrompt_MaxTokens_IsReasonable(t *testing.T) {
	// Suggestions don't need more than ~2000 tokens.
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	if req.MaxTokens > 2048 {
		t.Errorf("MaxTokens %d is larger than expected for suggestion output", req.MaxTokens)
	}
}

func TestInvariantSuggestionsPrompt_IsDeterministic(t *testing.T) {
	op := createOrderOp()
	req1 := prompt.InvariantSuggestionsPrompt(op)
	req2 := prompt.InvariantSuggestionsPrompt(op)
	if req1.SystemPrompt != req2.SystemPrompt {
		t.Error("SystemPrompt must be deterministic for the same operation")
	}
	if req1.UserPrompt != req2.UserPrompt {
		t.Error("UserPrompt must be deterministic for the same operation")
	}
}

func TestInvariantSuggestionsPrompt_LargeSchema_UserPromptUnder4000Chars(t *testing.T) {
	// Build an operation with a very large schema.
	props := map[string]model.Schema{}
	for i := 0; i < 100; i++ {
		props[strings.Repeat("field", i+1)] = model.Schema{Type: "string"}
	}
	op := model.Operation{
		ID:     "HugeOp",
		Method: "POST",
		Path:   "/huge",
		RequestBodySchema: &model.Schema{
			Type:       "object",
			Properties: props,
		},
	}
	req := prompt.InvariantSuggestionsPrompt(op)
	if len(req.UserPrompt) > 4000 {
		t.Errorf("UserPrompt exceeds 4000 chars for large schema: %d chars", len(req.UserPrompt))
	}
}

// --- StrategyRecommendationPrompt ---

func TestStrategyRecommendationPrompt_SystemPrompt_IsNonEmpty(t *testing.T) {
	req := prompt.StrategyRecommendationPrompt([]model.Operation{createOrderOp()})
	if req.SystemPrompt == "" {
		t.Error("SystemPrompt must not be empty")
	}
}

func TestStrategyRecommendationPrompt_SystemPrompt_InstructsJSONOnly(t *testing.T) {
	req := prompt.StrategyRecommendationPrompt([]model.Operation{createOrderOp()})
	lower := strings.ToLower(req.SystemPrompt)
	if !strings.Contains(lower, "json") {
		t.Error("SystemPrompt must instruct the model to respond in JSON")
	}
}

func TestStrategyRecommendationPrompt_UserPrompt_ContainsAllOperationIDs(t *testing.T) {
	ops := []model.Operation{createOrderOp(), getHealthOp()}
	req := prompt.StrategyRecommendationPrompt(ops)
	for _, op := range ops {
		if !strings.Contains(req.UserPrompt, op.ID) {
			t.Errorf("UserPrompt must include operation ID %q", op.ID)
		}
	}
}

func TestStrategyRecommendationPrompt_EmptyOperations_ProducesValidRequest(t *testing.T) {
	req := prompt.StrategyRecommendationPrompt([]model.Operation{})
	if req.SystemPrompt == "" {
		t.Error("SystemPrompt must not be empty even for empty operation list")
	}
	// MaxTokens must still be set.
	if req.MaxTokens <= 0 {
		t.Error("MaxTokens must be positive")
	}
}

func TestStrategyRecommendationPrompt_IsDeterministic(t *testing.T) {
	ops := []model.Operation{createOrderOp(), getHealthOp()}
	req1 := prompt.StrategyRecommendationPrompt(ops)
	req2 := prompt.StrategyRecommendationPrompt(ops)
	if req1.UserPrompt != req2.UserPrompt {
		t.Error("UserPrompt must be deterministic for the same operation list")
	}
}
```

---

## Step 3 — Response parser

### Spec

The response parser lives at `internal/ai/parse/`. It extracts structured data from the raw model text. This is a separate package because parsing is the most failure-prone step — model output is unreliable, and the parser must degrade gracefully.

- `ParseInvariantSuggestions(text string) ([]InvariantSuggestion, error)` — parses the model's JSON response into typed suggestions
- `ParseStrategyRecommendations(text string) ([]StrategyRecommendation, error)` — parses the model's JSON response into typed recommendations

- `InvariantSuggestion`:
  ```go
  type InvariantSuggestion struct {
      Name          string         `json:"name"`
      Rationale     string         `json:"rationale"`
      Confidence    string         `json:"confidence"`    // "high", "medium", or "low"
      InvariantType string         `json:"invariant_type"` // "no_5xx", "response_matches_schema", "idempotency", "custom"
      Config        map[string]any `json:"config,omitempty"`
  }
  ```

- `StrategyRecommendation` matches the M6 output schema (operation_id + strategies array)

- Parsing rules:
  - If the text is valid JSON starting with `[`, parse it directly as an array
  - If the text is wrapped in a markdown code fence (` ```json ... ``` `), strip the fence and parse the content — models frequently add these despite being told not to
  - If the text contains a JSON array embedded in prose, extract the first `[...]` block and parse it
  - If none of these work, return an empty slice and a descriptive error — never panic
  - After parsing, validate each suggestion: `confidence` must be one of `high`, `medium`, `low`; `invariant_type` must be one of the known values; `name` and `rationale` must be non-empty
  - Invalid suggestions are dropped with a warning, not returned — a partial result is better than an error

### Tests

`internal/ai/parse/parse_test.go`:

```go
package parse_test

import (
	"testing"

	"github.com/jimbery/bt/internal/ai/parse"
)

// --- ParseInvariantSuggestions ---

func TestParseInvariantSuggestions_ValidJSON_ReturnsSuggestions(t *testing.T) {
	text := `[
		{"name":"no_5xx","rationale":"Any server error indicates a bug in the handler.","confidence":"high","invariant_type":"no_5xx"},
		{"name":"response_matches_schema","rationale":"The 201 response must conform to the declared schema.","confidence":"high","invariant_type":"response_matches_schema"}
	]`
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 2 {
		t.Errorf("expected 2 suggestions, got %d", len(suggestions))
	}
}

func TestParseInvariantSuggestions_MarkdownFence_IsStripped(t *testing.T) {
	text := "```json\n[{\"name\":\"no_5xx\",\"rationale\":\"Server errors indicate bugs in the handler logic.\",\"confidence\":\"high\",\"invariant_type\":\"no_5xx\"}]\n```"
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error parsing fenced JSON: %v", err)
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 suggestion after fence strip, got %d", len(suggestions))
	}
}

func TestParseInvariantSuggestions_EmbeddedInProse_ExtractsArray(t *testing.T) {
	text := `Here are my suggestions for this operation:
[{"name":"no_5xx","rationale":"All endpoints should return non-500 responses for valid inputs.","confidence":"high","invariant_type":"no_5xx"}]
I recommend starting with the above.`
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) == 0 {
		t.Error("expected suggestions to be extracted from prose")
	}
}

func TestParseInvariantSuggestions_InvalidJSON_ReturnsError(t *testing.T) {
	_, err := parse.ParseInvariantSuggestions("this is not json")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParseInvariantSuggestions_EmptyString_ReturnsError(t *testing.T) {
	_, err := parse.ParseInvariantSuggestions("")
	if err == nil {
		t.Error("expected error for empty string")
	}
}

func TestParseInvariantSuggestions_InvalidConfidence_SuggestionIsDropped(t *testing.T) {
	// "extreme" is not a valid confidence value — the suggestion should be dropped.
	text := `[
		{"name":"no_5xx","rationale":"Valid suggestion with proper confidence value.","confidence":"high","invariant_type":"no_5xx"},
		{"name":"bad_one","rationale":"Invalid confidence.","confidence":"extreme","invariant_type":"custom"}
	]`
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 valid suggestion (invalid dropped), got %d", len(suggestions))
	}
	if suggestions[0].Name != "no_5xx" {
		t.Errorf("expected surviving suggestion to be 'no_5xx', got %q", suggestions[0].Name)
	}
}

func TestParseInvariantSuggestions_InvalidInvariantType_SuggestionIsDropped(t *testing.T) {
	text := `[
		{"name":"valid","rationale":"This is a valid suggestion with good rationale.","confidence":"high","invariant_type":"no_5xx"},
		{"name":"invalid","rationale":"Unknown invariant type.","confidence":"low","invariant_type":"unknown_type"}
	]`
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 valid suggestion, got %d", len(suggestions))
	}
}

func TestParseInvariantSuggestions_EmptyName_SuggestionIsDropped(t *testing.T) {
	text := `[
		{"name":"","rationale":"Has no name so must be dropped.","confidence":"high","invariant_type":"no_5xx"},
		{"name":"no_5xx","rationale":"Valid suggestion with a proper name and rationale.","confidence":"high","invariant_type":"no_5xx"}
	]`
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 suggestion (empty name dropped), got %d", len(suggestions))
	}
}

func TestParseInvariantSuggestions_EmptyRationale_SuggestionIsDropped(t *testing.T) {
	text := `[
		{"name":"no_5xx","rationale":"","confidence":"high","invariant_type":"no_5xx"},
		{"name":"schema_check","rationale":"Response body must match the declared schema definition.","confidence":"high","invariant_type":"response_matches_schema"}
	]`
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Errorf("expected 1 suggestion (empty rationale dropped), got %d", len(suggestions))
	}
}

func TestParseInvariantSuggestions_AllInvalid_ReturnsEmptySliceNotError(t *testing.T) {
	// All entries invalid — should return empty slice, not error.
	text := `[
		{"name":"","rationale":"","confidence":"extreme","invariant_type":"unknown"}
	]`
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 0 {
		t.Errorf("expected empty slice when all suggestions are invalid, got %d", len(suggestions))
	}
}

func TestParseInvariantSuggestions_WithConfig_ConfigIsPreserved(t *testing.T) {
	text := `[{
		"name":"amount_matches",
		"rationale":"The returned amount must equal the submitted amount for all valid requests.",
		"confidence":"medium",
		"invariant_type":"custom",
		"config":{"field":"amount","check":"equals_request_field"}
	}]`
	suggestions, err := parse.ParseInvariantSuggestions(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(suggestions))
	}
	cfg := suggestions[0].Config
	if cfg == nil {
		t.Fatal("expected Config to be populated")
	}
	if cfg["field"] != "amount" {
		t.Errorf("expected config.field='amount', got %v", cfg["field"])
	}
}

func TestParseInvariantSuggestions_DoesNotPanic_OnNullInput(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ParseInvariantSuggestions panicked: %v", r)
		}
	}()
	parse.ParseInvariantSuggestions("null")
}

// --- ParseStrategyRecommendations ---

func TestParseStrategyRecommendations_ValidJSON_ReturnsRecommendations(t *testing.T) {
	text := `[{
		"operation_id":"CreateOrder",
		"strategies":[
			{"strategy":"table","priority":"recommended","rationale":"Deterministic baseline for validation rules."},
			{"strategy":"property","priority":"recommended","rationale":"The currency enum and amount minimum benefit from property testing."}
		]
	}]`
	recs, err := parse.ParseStrategyRecommendations(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("expected 1 recommendation, got %d", len(recs))
	}
	if recs[0].OperationID != "CreateOrder" {
		t.Errorf("expected OperationID=CreateOrder, got %q", recs[0].OperationID)
	}
}

func TestParseStrategyRecommendations_MarkdownFence_IsStripped(t *testing.T) {
	text := "```json\n[{\"operation_id\":\"GetHealth\",\"strategies\":[{\"strategy\":\"table\",\"priority\":\"recommended\",\"rationale\":\"Health checks are simple deterministic cases.\"}]}]\n```"
	recs, err := parse.ParseStrategyRecommendations(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) == 0 {
		t.Error("expected at least one recommendation after fence strip")
	}
}

func TestParseStrategyRecommendations_InvalidPriority_StrategyIsDropped(t *testing.T) {
	text := `[{
		"operation_id":"GetHealth",
		"strategies":[
			{"strategy":"table","priority":"maybe","rationale":"Invalid priority value should be dropped."},
			{"strategy":"fuzz","priority":"optional","rationale":"Fuzz testing can find unexpected edge cases."}
		]
	}]`
	recs, err := parse.ParseStrategyRecommendations(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("expected 1 recommendation, got %d", len(recs))
	}
	if len(recs[0].Strategies) != 1 {
		t.Errorf("expected 1 valid strategy (invalid priority dropped), got %d", len(recs[0].Strategies))
	}
}

func TestParseStrategyRecommendations_EmptyOperationID_RecommendationIsDropped(t *testing.T) {
	text := `[
		{"operation_id":"","strategies":[{"strategy":"table","priority":"recommended","rationale":"Should be dropped."}]},
		{"operation_id":"GetHealth","strategies":[{"strategy":"table","priority":"recommended","rationale":"Valid recommendation."}]}
	]`
	recs, err := parse.ParseStrategyRecommendations(text)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(recs) != 1 {
		t.Errorf("expected 1 recommendation (empty ID dropped), got %d", len(recs))
	}
}
```

---

## Step 4 — `bt_suggest_invariants` tool

### Spec

`bt_suggest_invariants` is the new M7 tool, registered alongside the six from M6. It lives in `internal/mcp/tools/`.

**Description:** `"Given an operation ID from bt_discover_operations, suggest invariant candidates to add to your bt config. Returns structured suggestions with rationale and confidence for human review. Always includes no_5xx and response_matches_schema. Use bt_discover_operations first to get valid operation IDs."`

**Input schema:**
```json
{
  "type": "object",
  "required": ["schema_path", "operation_id"],
  "properties": {
    "schema_path": {
      "type": "string",
      "description": "Path to an OpenAPI 3.x schema file"
    },
    "operation_id": {
      "type": "string",
      "description": "The operationId to generate invariant suggestions for"
    }
  }
}
```

**Output schema:**
```json
{
  "type": "object",
  "required": ["operation_id", "suggestions", "provider"],
  "properties": {
    "operation_id": { "type": "string" },
    "provider":     { "type": "string", "description": "ai or stub — indicates whether AI was used" },
    "suggestions": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["name", "rationale", "confidence", "invariant_type"],
        "properties": {
          "name":           { "type": "string" },
          "rationale":      { "type": "string" },
          "confidence":     { "type": "string", "enum": ["high", "medium", "low"] },
          "invariant_type": { "type": "string", "enum": ["no_5xx", "response_matches_schema", "idempotency", "custom"] },
          "config":         { "type": "object" }
        }
      }
    }
  }
}
```

**Behaviour:**
- Discovers the named operation from the schema using the OpenAPI adapter
- If the operation ID is not found in the schema, returns a structured error: `{"error": "operation not found", "code": "OPERATION_NOT_FOUND", "operation_id": "<id>"}`
- Builds a prompt using `prompt.InvariantSuggestionsPrompt`
- Calls the configured `AIProvider`; if none is configured (no API key), uses `StubProvider` with a hardcoded response that includes `no_5xx` and `response_matches_schema`
- Parses the response using `parse.ParseInvariantSuggestions`
- Returns the suggestions with `provider: "ai"` or `provider: "stub"` to indicate which path was used
- If parsing fails entirely (model returned garbage), returns the stub suggestions with a warning logged — the tool never errors due to a bad model response

**Upgraded `bt_suggest_strategy` behaviour:**
- Same input/output schema as M6
- When an `AIProvider` is configured: builds a prompt using `prompt.StrategyRecommendationPrompt`, calls the provider, parses with `parse.ParseStrategyRecommendations`, and returns the result
- When no provider is configured: falls back to the M6 rule-based logic exactly — the output is identical in shape
- Adds `"provider": "ai"` or `"provider": "rules"` to the response to indicate which path was used

### Tests

`internal/mcp/tools/ai_tools_test.go`:

```go
package tools_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jimbery/bt/internal/ai"
	"github.com/jimbery/bt/internal/mcp/tools"
)

// stubSuggestionsJSON is a valid InvariantSuggestions response the stub returns.
const stubSuggestionsJSON = `[
	{"name":"no_5xx","rationale":"Any HTTP 5xx response from this operation indicates a server-side bug.","confidence":"high","invariant_type":"no_5xx"},
	{"name":"response_matches_schema","rationale":"The 201 response body must conform to the declared schema.","confidence":"high","invariant_type":"response_matches_schema"},
	{"name":"idempotency_key_prevents_duplicates","rationale":"POST operations should respect idempotency keys to prevent duplicate resource creation.","confidence":"medium","invariant_type":"idempotency"}
]`

func mustMarshalInput(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("cannot marshal: %v", err)
	}
	return b
}

func mustUnmarshalResult(t *testing.T, data json.RawMessage, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("cannot unmarshal: %v\nraw: %s", err, data)
	}
}

// --- bt_suggest_invariants ---

func TestSuggestInvariants_NoAPIKey_UsesStub(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		OperationID string `json:"operation_id"`
		Provider    string `json:"provider"`
		Suggestions []struct {
			Name          string `json:"name"`
			Rationale     string `json:"rationale"`
			Confidence    string `json:"confidence"`
			InvariantType string `json:"invariant_type"`
		} `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)

	if resp.OperationID != "CreateOrder" {
		t.Errorf("expected OperationID=CreateOrder, got %q", resp.OperationID)
	}
	if resp.Provider == "" {
		t.Error("provider field must be set")
	}
	if len(resp.Suggestions) == 0 {
		t.Error("expected at least one suggestion")
	}
}

func TestSuggestInvariants_Response_HasRequiredFields(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, _ := h(context.Background(), input)

	var resp map[string]any
	mustUnmarshalResult(t, result, &resp)

	for _, field := range []string{"operation_id", "provider", "suggestions"} {
		if _, ok := resp[field]; !ok {
			t.Errorf("response missing required field %q", field)
		}
	}
}

func TestSuggestInvariants_EachSuggestion_HasRequiredFields(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, _ := h(context.Background(), input)

	var resp struct {
		Suggestions []map[string]any `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)

	for i, s := range resp.Suggestions {
		for _, field := range []string{"name", "rationale", "confidence", "invariant_type"} {
			if v, ok := s[field]; !ok || v == "" {
				t.Errorf("suggestion[%d] missing or empty field %q", i, field)
			}
		}
	}
}

func TestSuggestInvariants_Confidence_IsValidEnum(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, _ := h(context.Background(), input)

	var resp struct {
		Suggestions []struct {
			Confidence string `json:"confidence"`
		} `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)

	valid := map[string]bool{"high": true, "medium": true, "low": true}
	for i, s := range resp.Suggestions {
		if !valid[s.Confidence] {
			t.Errorf("suggestion[%d] has invalid confidence %q (must be high/medium/low)", i, s.Confidence)
		}
	}
}

func TestSuggestInvariants_InvariantType_IsValidEnum(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, _ := h(context.Background(), input)

	var resp struct {
		Suggestions []struct {
			InvariantType string `json:"invariant_type"`
		} `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)

	valid := map[string]bool{
		"no_5xx":                 true,
		"response_matches_schema": true,
		"idempotency":            true,
		"custom":                 true,
	}
	for i, s := range resp.Suggestions {
		if !valid[s.InvariantType] {
			t.Errorf("suggestion[%d] has invalid invariant_type %q", i, s.InvariantType)
		}
	}
}

func TestSuggestInvariants_UnknownOperationID_ReturnsStructuredError(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "OperationThatDoesNotExist",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("expected structured error response, not Go error: %v", err)
	}
	var resp map[string]any
	mustUnmarshalResult(t, result, &resp)
	if resp["code"] != "OPERATION_NOT_FOUND" {
		t.Errorf("expected code OPERATION_NOT_FOUND, got %v", resp["code"])
	}
}

func TestSuggestInvariants_BadModelResponse_FallsBackToStub(t *testing.T) {
	// Model returns garbage — tool must not error, must return stub suggestions.
	garbageProvider := ai.NewStubProvider("this is not json at all and cannot be parsed")
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestInvariantsHandler(garbageProvider)

	input := mustMarshalInput(t, map[string]any{
		"schema_path":  schemaPath,
		"operation_id": "CreateOrder",
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("tool must not error on bad model response: %v", err)
	}
	var resp struct {
		Suggestions []any `json:"suggestions"`
	}
	mustUnmarshalResult(t, result, &resp)
	// Must still return at least the baseline suggestions.
	if len(resp.Suggestions) == 0 {
		t.Error("expected fallback suggestions even when model returns garbage")
	}
}

func TestSuggestInvariants_MissingSchemaPath_ReturnsValidationError(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	h := tools.SuggestInvariantsHandler(stub)
	input := mustMarshalInput(t, map[string]any{
		"operation_id": "CreateOrder",
		// schema_path missing
	})
	_, err := h(context.Background(), input)
	if err == nil {
		t.Error("expected error for missing schema_path")
	}
}

// --- upgraded bt_suggest_strategy ---

func TestSuggestStrategy_WithAIProvider_UsesAI(t *testing.T) {
	stubResponse := `[{
		"operation_id":"CreateOrder",
		"strategies":[
			{"strategy":"table","priority":"recommended","rationale":"The enum constraints on currency make deterministic table tests straightforward."},
			{"strategy":"property","priority":"recommended","rationale":"The amount minimum and currency enum are ideal for property-based boundary testing."}
		]
	}]`
	stub := ai.NewStubProvider(stubResponse)
	schemaPath := writeMinimalOpenAPISpec(t)
	h := tools.SuggestStrategyHandler(stub)

	input := mustMarshalInput(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Provider        string `json:"provider"`
		Recommendations []struct {
			OperationID string `json:"operation_id"`
			Strategies  []struct {
				Strategy  string `json:"strategy"`
				Rationale string `json:"rationale"`
			} `json:"strategies"`
		} `json:"recommendations"`
	}
	mustUnmarshalResult(t, result, &resp)
	_ = schemaPath

	if resp.Provider == "" {
		t.Error("expected provider field to be set")
	}
	if len(resp.Recommendations) == 0 {
		t.Error("expected at least one recommendation")
	}
	for _, rec := range resp.Recommendations {
		for _, s := range rec.Strategies {
			if s.Rationale == "" {
				t.Errorf("strategy %q has empty rationale", s.Strategy)
			}
		}
	}
}

func TestSuggestStrategy_WithNilProvider_UsesRuleBasedFallback(t *testing.T) {
	// nil provider means no AI configured — must use rule-based logic.
	h := tools.SuggestStrategyHandler(nil)
	input := mustMarshalInput(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "CreateOrder", "method": "POST", "has_body": true},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Provider        string `json:"provider"`
		Recommendations []any  `json:"recommendations"`
	}
	mustUnmarshalResult(t, result, &resp)
	if resp.Provider != "rules" {
		t.Errorf("expected provider='rules' when no AI configured, got %q", resp.Provider)
	}
	if len(resp.Recommendations) == 0 {
		t.Error("expected recommendations from rule-based fallback")
	}
}

func TestSuggestStrategy_BadAIResponse_FallsBackToRules(t *testing.T) {
	garbage := ai.NewStubProvider("not valid json for strategy recommendations")
	h := tools.SuggestStrategyHandler(garbage)
	input := mustMarshalInput(t, map[string]any{
		"operations": []any{
			map[string]any{"id": "GetHealth", "method": "GET", "has_body": false},
		},
	})
	result, err := h(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var resp struct {
		Recommendations []any `json:"recommendations"`
	}
	mustUnmarshalResult(t, result, &resp)
	if len(resp.Recommendations) == 0 {
		t.Error("expected rule-based fallback recommendations when AI returns garbage")
	}
}

// --- AllExpectedTools includes bt_suggest_invariants ---

func TestAllExpectedTools_IncludesSuggestInvariants(t *testing.T) {
	stub := ai.NewStubProvider(stubSuggestionsJSON)
	allTools := tools.AllWithProvider(stub)
	found := false
	for _, tool := range allTools {
		if tool.Name == "bt_suggest_invariants" {
			found = true
		}
	}
	if !found {
		t.Error("expected bt_suggest_invariants to be registered")
	}
}
```

---

## Step 5 — Config and CLI integration

### Spec

- `~/.config/bt/config.yaml` is read on startup; it may contain:
  ```yaml
  ai:
    provider: anthropic          # only supported value in M7
    api_key: sk-ant-...          # or set ANTHROPIC_API_KEY env var
    model: claude-sonnet-4-5     # optional, defaults to claude-sonnet-4-5
    max_tokens: 1024             # optional
  ```
- `ANTHROPIC_API_KEY` environment variable takes precedence over the config file value
- If neither is set, the stub provider is used silently — no warning unless `--verbose` is passed
- `bt mcp serve` reads the AI config at startup and injects the configured provider into all AI-backed tool handlers
- `bt mcp call bt_suggest_invariants` works the same way

### Tests

`internal/ai/config_test.go`:

```go
package ai_test

import (
	"os"
	"testing"

	"github.com/jimbery/bt/internal/ai"
)

func TestLoadProviderConfig_EnvVarTakesPrecedence(t *testing.T) {
	os.Setenv("ANTHROPIC_API_KEY", "env-key")
	t.Cleanup(func() { os.Unsetenv("ANTHROPIC_API_KEY") })

	cfg, err := ai.LoadProviderConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "env-key" {
		t.Errorf("expected API key from env var, got %q", cfg.APIKey)
	}
}

func TestLoadProviderConfig_NoKeySet_ReturnsEmptyKey(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	cfg, err := ai.LoadProviderConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.APIKey != "" {
		t.Errorf("expected empty API key when none configured, got %q", cfg.APIKey)
	}
}

func TestLoadProviderConfig_EmptyKey_DefaultModelIsSet(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	cfg, _ := ai.LoadProviderConfig()
	if cfg.Model == "" {
		t.Error("default model must be set even when no key is configured")
	}
}

func TestLoadProviderConfig_DefaultModel_IsClaude(t *testing.T) {
	os.Unsetenv("ANTHROPIC_API_KEY")
	cfg, _ := ai.LoadProviderConfig()
	if cfg.Model != "claude-sonnet-4-5" {
		t.Errorf("expected default model 'claude-sonnet-4-5', got %q", cfg.Model)
	}
}
```

---

## Local verification

```bash
# Unit tests — all AI packages
go test ./internal/ai/... -race -v
go test ./internal/ai/prompt/... -race -v
go test ./internal/ai/parse/... -race -v
go test ./internal/mcp/tools/... -race -v

# Build and smoke test without an API key (stub path)
go build -o bt ./cmd/bt
./bt mcp call bt_suggest_invariants \
  --input '{"schema_path":"examples/orders-api/openapi.yaml","operation_id":"CreateOrder"}'

# With an API key set (real path)
ANTHROPIC_API_KEY=sk-ant-... ./bt mcp call bt_suggest_invariants \
  --input '{"schema_path":"examples/orders-api/openapi.yaml","operation_id":"CreateOrder"}'
```

---

## Model additions required

No new domain model fields are required for M7. The AI layer operates on existing `model.Operation` and `model.Schema` types, and returns new types (`InvariantSuggestion`, `StrategyRecommendation`) that live in `internal/ai/parse/`, not in `pkg/model/`.

---

## M7 exit criterion

Pointing an MCP client at the orders API schema:

1. `bt_suggest_invariants` with `operation_id: CreateOrder` returns at least three suggestions including `no_5xx`, `response_matches_schema`, and one additional candidate
2. Every suggestion has a non-empty `rationale`, a valid `confidence`, and a valid `invariant_type`
3. The `provider` field is `"stub"` when no API key is set, `"ai"` when one is
4. `bt_suggest_strategy` returns richer rationale when an AI provider is configured, and falls back to rule-based output when none is
5. A bad model response (garbage JSON) never causes a tool error — it falls back to stub suggestions silently
6. All unit tests pass with `-race`