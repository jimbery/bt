package main

import (
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"
)

var validStatuses = map[string]bool{
	"pending":   true,
	"confirmed": true,
	"shipped":   true,
	"delivered": true,
	"cancelled": true,
}

var brokenCallCount atomic.Uint64
var deleteAttempts atomic.Uint64

func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func handleListOrders(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		statusFilter := r.URL.Query().Get("status")
		if statusFilter != "" && !validStatuses[statusFilter] {
			writeError(w, http.StatusBadRequest, "INVALID_STATUS",
				"status must be one of: pending, confirmed, shipped, delivered, cancelled")
			return
		}
		orders := s.list(statusFilter)
		if orders == nil {
			orders = []*order{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"orders": orders})
	}
}

func handleCreateOrder(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Amount      int    `json:"amount"`
			Currency    string `json:"currency"`
			Description string `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}
		if req.Amount <= 0 {
			writeError(w, http.StatusBadRequest, "INVALID_AMOUNT", "amount must be a positive integer")
			return
		}
		if req.Currency == "" {
			writeError(w, http.StatusBadRequest, "MISSING_CURRENCY", "currency is required")
			return
		}
		o := s.create(req.Amount, req.Currency, req.Description, r.Header.Get("Idempotency-Key"))
		writeJSON(w, http.StatusCreated, o)
	}
}

func handleGetOrder(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		o, ok := s.get(id)
		if !ok {
			writeError(w, http.StatusNotFound, "ORDER_NOT_FOUND",
				"order "+id+" not found")
			return
		}
		writeJSON(w, http.StatusOK, o)
	}
}

func handleUpdateOrder(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var req struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_BODY", "request body must be valid JSON")
			return
		}
		if !validStatuses[req.Status] {
			writeError(w, http.StatusBadRequest, "INVALID_STATUS",
				"status must be one of: pending, confirmed, shipped, delivered, cancelled")
			return
		}
		o, ok := s.update(id, req.Status)
		if !ok {
			writeError(w, http.StatusNotFound, "ORDER_NOT_FOUND",
				"order "+id+" not found")
			return
		}
		if os.Getenv("ORDERS_API_TIMESTAMP_BUG") == "1" && req.Status == "cancelled" {
			var ts int64
			if o.CancelledAt != nil {
				ts = o.CancelledAt.Unix()
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id":           o.ID,
				"amount":       o.Amount,
				"currency":     o.Currency,
				"description":  o.Description,
				"status":       o.Status,
				"created_at":   o.CreatedAt,
				"cancelled_at": ts,
			})
			return
		}
		writeJSON(w, http.StatusOK, o)
	}
}

func handleBrokenOrder(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := brokenCallCount.Add(1)
		id := r.PathValue("id")
		o, ok := s.get(id)
		if !ok {
			// Self-contained broken responses for unknown IDs (M3.5 replay smoke tests).
			if n%2 == 0 {
				writeJSON(w, http.StatusOK, map[string]any{
					"id":     id,
					"status": "unknown",
				})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id": id,
			})
			return
		}
		// Known order: always return an Order-shaped response that violates the schema
		// (missing required fields) so contract runs are deterministic.
		writeJSON(w, http.StatusOK, map[string]any{
			"id": o.ID,
		})
	}
}

func handleDeleteOrder(s *store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		deleteAttempts.Add(1)
		if r.Header.Get("X-Confirm-Delete") != "yes" {
			writeError(w, http.StatusBadRequest, "MISSING_CONFIRM", "confirmation header required")
			return
		}
		id := r.PathValue("id")
		if !s.delete(id) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "not found")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleAdminDeleteCount(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"delete_attempts": deleteAttempts.Load()})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(mustMarshal(body))
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
		"code":  code,
	})
}
