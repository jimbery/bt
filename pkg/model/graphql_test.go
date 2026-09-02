package model_test

import (
	"encoding/json"
	"testing"

	"github.com/jimbery/bt/pkg/model"
)

func TestCaseInput_GQLFields_RoundTripWithoutLoss(t *testing.T) {
	t.Parallel()
	original := model.CaseInput{
		Method: "POST",
		Path:   "/graphql",
		Headers: map[string]string{
			"Content-Type": "application/json",
		},
		GQLQuery:         `query GetProduct($id: ID!) { product(id: $id) { id name price } }`,
		GQLOperationName: "GetProduct",
		GQLVariables:     map[string]any{"id": "prod-001"},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.CaseInput
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.GQLQuery != original.GQLQuery {
		t.Errorf("GQLQuery: got %q, want %q", decoded.GQLQuery, original.GQLQuery)
	}
	if decoded.GQLOperationName != original.GQLOperationName {
		t.Errorf("GQLOperationName: got %q, want %q", decoded.GQLOperationName, original.GQLOperationName)
	}
	if decoded.GQLVariables["id"] != original.GQLVariables["id"] {
		t.Errorf("GQLVariables[id]: got %v, want %v", decoded.GQLVariables["id"], original.GQLVariables["id"])
	}
}

func TestCaseInput_RESTFields_UnchangedByGQLAdditions(t *testing.T) {
	t.Parallel()
	rest := model.CaseInput{
		Method:  "GET",
		Path:    "/orders/1",
		Headers: map[string]string{"Authorization": "Bearer token"},
	}

	data, err := json.Marshal(rest)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}
	for _, field := range []string{"gql_query", "gql_operation_name", "gql_variables"} {
		if _, present := raw[field]; present {
			t.Errorf("REST CaseInput must not include %q in JSON output when zero", field)
		}
	}
}

func TestCaseInput_GQLQueryPresent_SignalsGraphQLCase(t *testing.T) {
	t.Parallel()
	restCase := model.CaseInput{Method: "GET", Path: "/orders"}
	gqlCase := model.CaseInput{
		Method:   "POST",
		Path:     "/graphql",
		GQLQuery: `{ products { id } }`,
	}

	if restCase.IsGraphQL() {
		t.Error("REST CaseInput must not be identified as GraphQL")
	}
	if !gqlCase.IsGraphQL() {
		t.Error("CaseInput with GQLQuery must be identified as GraphQL")
	}
}

func TestOperation_GQLFields_RoundTripWithoutLoss(t *testing.T) {
	t.Parallel()
	original := model.Operation{
		ID:          "GetProduct",
		Method:      "POST",
		Path:        "/graphql",
		GQLKind:     model.GQLQuery,
		GQLDocument: `query GetProduct($id: ID!) { product(id: $id) { id name price } }`,
		GQLVariableTypes: map[string]*model.SchemaRef{
			"id": {Type: "string"},
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded model.Operation
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.GQLKind != model.GQLQuery {
		t.Errorf("GQLKind: got %q, want %q", decoded.GQLKind, model.GQLQuery)
	}
	if decoded.GQLDocument != original.GQLDocument {
		t.Errorf("GQLDocument: got %q, want %q", decoded.GQLDocument, original.GQLDocument)
	}
	if decoded.GQLVariableTypes["id"].Type != "string" {
		t.Errorf("GQLVariableTypes[id].Type: got %q, want string", decoded.GQLVariableTypes["id"].Type)
	}
}

func TestOperation_RESTOperation_GQLFieldsAbsentFromJSON(t *testing.T) {
	t.Parallel()
	rest := model.Operation{
		ID:     "GetOrder",
		Method: "GET",
		Path:   "/orders/{id}",
	}

	data, err := json.Marshal(rest)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal to map failed: %v", err)
	}
	for _, field := range []string{"gql_kind", "gql_document", "gql_variable_types", "gql_selection_schema"} {
		if _, present := raw[field]; present {
			t.Errorf("REST Operation must not include %q in JSON output when zero", field)
		}
	}
}

func TestGQLOperationKind_Constants_HaveExpectedValues(t *testing.T) {
	t.Parallel()
	cases := map[model.GQLOperationKind]string{
		model.GQLQuery:        "Query",
		model.GQLMutation:     "Mutation",
		model.GQLSubscription: "Subscription",
	}
	for kind, want := range cases {
		if string(kind) != want {
			t.Errorf("GQLOperationKind %v: got %q, want %q", kind, string(kind), want)
		}
	}
}
