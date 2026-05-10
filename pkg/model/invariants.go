package model

// Built-in failure / invariant names produced by table evaluation and replay.
const (
	InvariantStatusCode     = "status_code"
	InvariantResponseHeader = "response_header"
)

// Property-strategy invariant names.
const (
	InvariantNo5xx                       = "no_5xx"
	InvariantResponseMatchesSchema       = "response_matches_schema"
	InvariantIdempotencyKeyPreventsDupes = "idempotency_key_prevents_duplicates"
	InvariantContract                    = "contract"
)

// IdempotencyResult captures two HTTP responses for the same idempotent request pair.
type IdempotencyResult struct {
	IdempotencyKey string         `json:"idempotency_key"`
	First          ResponseDetail `json:"first"`
	Second         ResponseDetail `json:"second"`
}
