// Command graphql-api is a minimal GraphQL HTTP server for M9.5 integration tests.
//
// Listen: --port or PORT (default 8090). POST /graphql, GET /health.
// Set BT_GQL_AMOUNT_BUG=1 to deliberately return amount as a string in JSON responses.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"sync"

	"github.com/graphql-go/graphql"
)

type gqlHTTPRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

var (
	schemaOnce sync.Once
	gqlSchema  graphql.Schema
	schemaErr  error
)

func loadSchema() (graphql.Schema, error) {
	schemaOnce.Do(func() {
		gqlSchema, schemaErr = buildSchema()
	})
	return gqlSchema, schemaErr
}

// NewHandler returns the HTTP handler for POST /graphql and GET /health.
func NewHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", healthHTTP)
	mux.HandleFunc("POST /graphql", serveGraphQL)
	return mux
}

func healthHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func serveGraphQL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req gqlHTTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errors": []any{map[string]any{"message": "invalid JSON body"}},
		})
		return
	}

	schema, err := loadSchema()
	if err != nil {
		log.Printf("graphql schema: %v", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	result := graphql.Do(graphql.Params{
		Schema:         schema,
		RequestString:  req.Query,
		VariableValues: req.Variables,
		OperationName:  req.OperationName,
		Context:        r.Context(),
	})

	if result.Data != nil {
		corruptAmountBug(result.Data)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	out := map[string]any{"data": result.Data}
	if len(result.Errors) > 0 {
		out["errors"] = result.Errors
	}
	if err := json.NewEncoder(w).Encode(out); err != nil {
		log.Printf("encode graphql response: %v", err)
	}
}

func main() {
	portFlag := flag.String("port", "", "listen port (default: PORT env or 8090)")
	flag.Parse()
	port := *portFlag
	if port == "" {
		port = os.Getenv("PORT")
	}
	if port == "" {
		port = "8090"
	}
	addr := ":" + port
	if ep := os.Getenv("ADDR"); ep != "" {
		addr = ep
	}

	log.Printf("graphql-api listening on %s (POST /graphql, GET /health)", addr)
	log.Fatal(http.ListenAndServe(addr, NewHandler()))
}
