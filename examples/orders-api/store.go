package main

import (
	"fmt"
	"sync"
	"time"
)

type order struct {
	ID          string    `json:"id"`
	Amount      int       `json:"amount"`
	Currency    string    `json:"currency"`
	Description string    `json:"description,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type store struct {
	mu     sync.RWMutex
	orders map[string]*order
	seq    int
}

func newStore() *store {
	return &store{orders: make(map[string]*order)}
}

func (s *store) create(amount int, currency, description string) *order {
	s.mu.Lock()
	defer s.mu.Unlock()

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
	return o, true
}
