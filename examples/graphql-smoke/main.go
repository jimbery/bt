// Command graphql-smoke is a tiny GraphQL HTTP server for exercising bt with adapter: graphql.
//
// Start: go run ./examples/graphql-smoke
// Listen: ADDR env (default :8099). Then from repo root:
//
//	./bt run --config examples/graphql-smoke/bt/backendtest.yaml --strategy table --adapter graphql
package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
)

type gqlRequest struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName"`
	Variables     map[string]any `json:"variables"`
}

type widget struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

var (
	mu       sync.Mutex
	widgets  = map[string]widget{"w1": {ID: "w1", Name: "seed"}}
	idSerial int64 = 1
)

func main() {
	addr := ":8099"
	if v := strings.TrimSpace(os.Getenv("ADDR")); v != "" {
		addr = v
	}
	http.HandleFunc("/graphql", graphql)
	log.Printf("graphql-smoke listening on %s (POST /graphql)", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func graphql(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req gqlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"errors": []any{map[string]any{"message": "invalid JSON body"}},
		})
		return
	}
	q := req.Query
	w.Header().Set("Content-Type", "application/json")

	switch {
	case strings.Contains(q, "createWidget"):
		name, _ := req.Variables["name"].(string)
		if strings.TrimSpace(name) == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"data":   nil,
				"errors": []any{map[string]any{"message": "name required"}},
			})
			return
		}
		mu.Lock()
		idSerial++
		id := "w" + strconv.FormatInt(idSerial, 10)
		wid := widget{ID: id, Name: name}
		widgets[id] = wid
		mu.Unlock()
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"createWidget": map[string]any{"id": wid.ID, "name": wid.Name},
			},
		})
	case strings.Contains(q, "widget("):
		id, _ := req.Variables["id"].(string)
		mu.Lock()
		wid, ok := widgets[id]
		mu.Unlock()
		if !ok {
			writeJSON(w, http.StatusOK, map[string]any{
				"data":   map[string]any{"widget": nil},
				"errors": []any{map[string]any{"message": "widget not found"}},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{
				"widget": map[string]any{"id": wid.ID, "name": wid.Name},
			},
		})
	case strings.Contains(q, "ping"):
		writeJSON(w, http.StatusOK, map[string]any{
			"data": map[string]any{"ping": "ok"},
		})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"errors": []any{map[string]any{"message": "unsupported query"}},
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
