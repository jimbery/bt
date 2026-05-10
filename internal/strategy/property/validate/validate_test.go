package validate_test

import (
	"encoding/json"
	"testing"

	"github.com/jayimbery/bt/internal/strategy/property/validate"
	"github.com/jayimbery/bt/pkg/model"
)

func TestValidateResponse_ValidObject(t *testing.T) {
	t.Parallel()
	schema := &model.SchemaRef{
		Type: "object",
		Properties: map[string]*model.SchemaRef{
			"id": {Type: "string"},
		},
		Required: []string{"id"},
	}
	body, _ := json.Marshal(map[string]any{"id": "a"})
	if v := validate.ValidateResponse(body, schema); len(v) != 0 {
		t.Fatalf("violations: %v", v)
	}
}

func TestValidateResponse_MissingRequired(t *testing.T) {
	t.Parallel()
	schema := &model.SchemaRef{
		Type: "object",
		Properties: map[string]*model.SchemaRef{
			"id": {Type: "string"},
		},
		Required: []string{"id"},
	}
	body := []byte(`{}`)
	v := validate.ValidateResponse(body, schema)
	if len(v) == 0 {
		t.Fatal("expected violation")
	}
}
