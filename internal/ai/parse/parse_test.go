package parse_test

import (
	"testing"

	"github.com/jayimbery/bt/internal/ai/parse"
)

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
	_, _ = parse.ParseInvariantSuggestions("null")
}

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
