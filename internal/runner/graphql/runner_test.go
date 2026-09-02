package graphql_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	gqlrunner "github.com/jimbery/bt/internal/runner/graphql"
	"github.com/jimbery/bt/pkg/model"
)

func gqlServer(t *testing.T, status int, responseBody string, capture *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			_ = json.NewDecoder(r.Body).Decode(capture)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(responseBody))
	}))
}

func TestGQLRunner_ValidQuery_SendsPOSTWithJSONBody(t *testing.T) {
	t.Parallel()
	var received map[string]any
	srv := gqlServer(t, 200, `{"data":{"product":{"id":"prod-1","name":"Widget"}}}`, &received)
	defer srv.Close()
	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:           "POST",
		Path:             "/graphql",
		GQLQuery:         `query GetProduct($id: ID!) { product(id: $id) { id name } }`,
		GQLOperationName: "GetProduct",
		GQLVariables:     map[string]any{"id": "prod-1"},
	}
	resp, err := r.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if received["query"] == nil {
		t.Error("request must include 'query' field")
	}
	if received["operationName"] != "GetProduct" {
		t.Errorf("expected operationName 'GetProduct', got %v", received["operationName"])
	}
	vars, ok := received["variables"].(map[string]any)
	if !ok {
		t.Fatal("expected 'variables' to be an object")
	}
	if vars["id"] != "prod-1" {
		t.Errorf("expected variable id='prod-1', got %v", vars["id"])
	}
}

func TestGQLRunner_NoOperationName_OmitsOperationNameFromBody(t *testing.T) {
	t.Parallel()
	var received map[string]any
	srv := gqlServer(t, 200, `{"data":{"ping":"pong"}}`, &received)
	defer srv.Close()
	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ ping }`,
	}
	if _, err := r.Run(context.Background(), input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := received["operationName"]; present {
		t.Error("operationName must be omitted from request body when empty")
	}
}

func TestGQLRunner_NoVariables_OmitsVariablesFromBody(t *testing.T) {
	t.Parallel()
	var received map[string]any
	srv := gqlServer(t, 200, `{"data":{"ping":"pong"}}`, &received)
	defer srv.Close()
	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ ping }`,
	}
	if _, err := r.Run(context.Background(), input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, present := received["variables"]; present {
		t.Error("variables must be omitted from request body when nil")
	}
}

func TestGQLRunner_ResponseBodyPreservedRaw(t *testing.T) {
	t.Parallel()
	rawBody := `{"data":{"product":{"id":"prod-1","name":"Widget","price":9.99}}}`
	srv := gqlServer(t, 200, rawBody, nil)
	defer srv.Close()
	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ product(id:"prod-1") { id name price } }`,
	}
	resp, err := r.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(resp.Body) != rawBody {
		t.Errorf("expected raw body %q, got %q", rawBody, string(resp.Body))
	}
}

func TestGQLRunner_GraphQLErrorsIn200_ReturnsResponseWithoutError(t *testing.T) {
	t.Parallel()
	body := `{"data":null,"errors":[{"message":"product not found","locations":[{"line":1,"column":3}],"path":["product"]}]}`
	srv := gqlServer(t, 200, body, nil)
	defer srv.Close()
	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ product(id:"does-not-exist") { id } }`,
	}
	resp, err := r.Run(context.Background(), input)
	if err != nil {
		t.Fatalf("GraphQL-level errors must not be treated as transport errors, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(resp.Body) == 0 {
		t.Error("response body must be non-empty even when errors are present")
	}
}

func TestGQLRunner_NonGraphQLInput_ReturnsErrNotGraphQL(t *testing.T) {
	t.Parallel()
	r := gqlrunner.New(gqlrunner.Config{BaseURL: "http://localhost"})
	input := model.CaseInput{
		Method: "GET",
		Path:   "/orders",
	}
	_, err := r.Run(context.Background(), input)
	if err == nil {
		t.Fatal("expected ErrNotGraphQL for non-GraphQL input, got nil")
	}
	if err != gqlrunner.ErrNotGraphQL {
		t.Errorf("expected ErrNotGraphQL, got %v", err)
	}
}

func TestGQLRunner_ContextCancelled_ReturnsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	input := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ ping }`,
	}
	_, err := r.Run(ctx, input)
	if err == nil {
		t.Error("expected error when context is cancelled, got nil")
	}
}

func TestGQLRunner_SetsContentTypeApplicationJSON(t *testing.T) {
	t.Parallel()
	var receivedContentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	r := gqlrunner.New(gqlrunner.Config{BaseURL: srv.URL})
	_, err := r.Run(context.Background(), model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ ping }`,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedContentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", receivedContentType)
	}
}
