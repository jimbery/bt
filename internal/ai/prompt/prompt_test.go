package prompt_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jimbery/bt/internal/ai/prompt"
	"github.com/jimbery/bt/pkg/model"
)

func createOrderOp() model.Operation {
	min := 0.01
	return model.Operation{
		ID:     "CreateOrder",
		Method: "POST",
		Path:   "/orders",
		RequestBody: &model.SchemaRef{
			Type: "object",
			Properties: map[string]*model.SchemaRef{
				"amount":      {Type: "number", Minimum: &min},
				"currency":    {Type: "string", Enum: []any{"GBP", "USD", "EUR"}},
				"description": {Type: "string", Nullable: true},
			},
			Required: []string{"amount", "currency"},
		},
		Responses: []model.ResponseSpec{
			{
				StatusCode: 201,
				Schema: &model.SchemaRef{
					Type: "object",
					Properties: map[string]*model.SchemaRef{
						"id":       {Type: "string"},
						"amount":   {Type: "number"},
						"currency": {Type: "string"},
						"status":   {Type: "string", Enum: []any{"pending", "complete", "cancelled"}},
					},
					Required: []string{"id", "amount", "currency", "status"},
				},
			},
			{
				StatusCode: 400,
				Schema: &model.SchemaRef{
					Type:       "object",
					Properties: map[string]*model.SchemaRef{"error": {Type: "string"}, "code": {Type: "string"}},
					Required:   []string{"error", "code"},
				},
			},
		},
	}
}

func getHealthOp() model.Operation {
	return model.Operation{
		ID:     "GetHealth",
		Method: "GET",
		Path:   "/health",
		Responses: []model.ResponseSpec{
			{
				StatusCode: 200,
				Schema: &model.SchemaRef{
					Type:       "object",
					Properties: map[string]*model.SchemaRef{"status": {Type: "string"}},
					Required:   []string{"status"},
				},
			},
		},
	}
}

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
	if !strings.Contains(req.UserPrompt, "amount") {
		t.Error("UserPrompt must include request body field names")
	}
}

func TestInvariantSuggestionsPrompt_UserPrompt_IsValidJSON_WhenSchemaIncluded(t *testing.T) {
	req := prompt.InvariantSuggestionsPrompt(createOrderOp())
	idx := strings.Index(req.UserPrompt, "{")
	if idx == -1 {
		t.Skip("UserPrompt does not contain a JSON block")
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
	props := map[string]*model.SchemaRef{}
	for i := 0; i < 100; i++ {
		key := strings.Repeat("f", i+1)
		props[key] = &model.SchemaRef{Type: "string"}
	}
	op := model.Operation{
		ID:     "HugeOp",
		Method: "POST",
		Path:   "/huge",
		RequestBody: &model.SchemaRef{
			Type:       "object",
			Properties: props,
		},
	}
	req := prompt.InvariantSuggestionsPrompt(op)
	if len(req.UserPrompt) > 4000 {
		t.Errorf("UserPrompt exceeds 4000 chars for large schema: %d chars", len(req.UserPrompt))
	}
}

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
