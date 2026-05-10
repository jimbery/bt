package spec

import (
	"os"
	"strings"
	"testing"
)

func TestOpenAPISpec_BrokenEndpoint_HasResponseSchema(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("cannot read openapi.yaml: %v", err)
	}

	spec := string(data)
	requiredPhrases := []string{
		"GetOrderBroken",
		"/orders/{id}/broken",
		"required:",
		"amount",
		"currency",
		"status",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(spec, phrase) {
			t.Errorf("openapi.yaml is missing expected phrase %q — broken endpoint schema may be incomplete", phrase)
		}
	}
}

func TestOpenAPISpec_CreateEndpoint_HasRequestBodyAndResponseSchema(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("cannot read openapi.yaml: %v", err)
	}

	spec := string(data)
	requiredPhrases := []string{
		"CreateOrder",
		"requestBody",
		"\"201\"",
		"\"400\"",
	}
	for _, phrase := range requiredPhrases {
		if !strings.Contains(spec, phrase) {
			t.Errorf("openapi.yaml is missing expected phrase %q for CreateOrder", phrase)
		}
	}
}
