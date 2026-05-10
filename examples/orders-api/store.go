package main

import (
	"fmt"
	"sync"
	"time"
)

type order struct {
	ID           string     `json:"id"`
	Amount       int        `json:"amount"`
	Currency     string     `json:"currency"`
	Description  string     `json:"description,omitempty"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	CancelledAt  *time.Time `json:"cancelled_at,omitempty"`
}

type store struct {
	mu     sync.RWMutex
	orders map[string]*order
	seq    int
	// idem maps Idempotency-Key header value to order ID for replay-safe creates.
	idem map[string]string
}

func newStore() *store {
	return &store{orders: make(map[string]*order), idem: make(map[string]string)}
}

func (s *store) create(amount int, currency, description, idemKey string) *order {
	s.mu.Lock()
	defer s.mu.Unlock()

	if idemKey != "" {
		if id, ok := s.idem[idemKey]; ok {
			return s.orders[id]
		}
	}

	s.seq++
	o := &order{
		ID:          fmt.Sprintf("ord-%04d", s.seq),
		Amount:      amount,
		Currency:    currency,
		Description: description,
		Status:      "pending",
		CreatedAt:   time.Now().UTC(),
	}
	s.orders[o.ID] = o
	if idemKey != "" {
		s.idem[idemKey] = o.ID
	}
	return o
}

func (s *store) get(id string) (*order, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	o, ok := s.orders[id]
	return o, ok
}

func (s *store) list(statusFilter string) []*order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]*order, 0, len(s.orders))
	for _, o := range s.orders {
		if statusFilter == "" || o.Status == statusFilter {
			out = append(out, o)
		}
	}
	return out
}

func (s *store) update(id, status string) (*order, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	o, ok := s.orders[id]
	if !ok {
		return nil, false
	}
	o.Status = status
	if status == "cancelled" {
		t := time.Now().UTC()
		o.CancelledAt = &t
	} else {
		o.CancelledAt = nil
	}
	return o, true
}

func (s *store) delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.orders[id]; !ok {
		return false
	}
	delete(s.orders, id)
	return true
}
