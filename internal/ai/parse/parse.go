// Package parse extracts structured AI tool output from raw model text.
package parse

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
)

// InvariantSuggestion is one invariant candidate from the model.
type InvariantSuggestion struct {
	Name          string         `json:"name"`
	Rationale     string         `json:"rationale"`
	Confidence    string         `json:"confidence"`
	InvariantType string         `json:"invariant_type"`
	Config        map[string]any `json:"config,omitempty"`
}

// StrategyRec is one operation's strategy recommendations (M6 shape).
type StrategyRec struct {
	OperationID string           `json:"operation_id"`
	Strategies  []StrategyChoice `json:"strategies"`
}

// StrategyChoice is one strategy line in a recommendation.
type StrategyChoice struct {
	Strategy  string `json:"strategy"`
	Priority  string `json:"priority"`
	Rationale string `json:"rationale"`
}

var validConfidence = map[string]bool{"high": true, "medium": true, "low": true}
var validInvariantType = map[string]bool{
	"no_5xx": true, "response_matches_schema": true, "idempotency": true, "custom": true,
}
var validPriority = map[string]bool{
	"recommended": true, "optional": true, "not_applicable": true,
}
var validStrategy = map[string]bool{
	"table": true, "property": true, "fuzz": true, "contract": true,
}

// ParseInvariantSuggestions parses JSON (optionally fenced or embedded) into suggestions.
func ParseInvariantSuggestions(text string) ([]InvariantSuggestion, error) {
	raw, err := extractJSONArray(text)
	if err != nil {
		return nil, err
	}
	var items []InvariantSuggestion
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse invariant suggestions: %w", err)
	}
	out := make([]InvariantSuggestion, 0, len(items))
	for _, it := range items {
		if strings.TrimSpace(it.Name) == "" || strings.TrimSpace(it.Rationale) == "" {
			log.Printf("bt ai: dropping invariant suggestion: empty name or rationale")
			continue
		}
		if !validConfidence[it.Confidence] {
			log.Printf("bt ai: dropping invariant suggestion %q: invalid confidence %q", it.Name, it.Confidence)
			continue
		}
		if !validInvariantType[it.InvariantType] {
			log.Printf("bt ai: dropping invariant suggestion %q: invalid invariant_type %q", it.Name, it.InvariantType)
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

// ParseStrategyRecommendations parses JSON into strategy recommendations.
func ParseStrategyRecommendations(text string) ([]StrategyRec, error) {
	raw, err := extractJSONArray(text)
	if err != nil {
		return nil, err
	}
	var items []StrategyRec
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("parse strategy recommendations: %w", err)
	}
	out := make([]StrategyRec, 0, len(items))
	for _, rec := range items {
		if strings.TrimSpace(rec.OperationID) == "" {
			log.Printf("bt ai: dropping strategy recommendation with empty operation_id")
			continue
		}
		strats := make([]StrategyChoice, 0, len(rec.Strategies))
		for _, s := range rec.Strategies {
			if strings.TrimSpace(s.Strategy) == "" || strings.TrimSpace(s.Rationale) == "" {
				continue
			}
			if !validStrategy[s.Strategy] {
				log.Printf("bt ai: dropping strategy %q: unknown strategy name", s.Strategy)
				continue
			}
			if !validPriority[s.Priority] {
				log.Printf("bt ai: dropping strategy %q: invalid priority %q", s.Strategy, s.Priority)
				continue
			}
			strats = append(strats, s)
		}
		if len(strats) == 0 {
			continue
		}
		rec.Strategies = strats
		out = append(out, rec)
	}
	return out, nil
}

func extractJSONArray(text string) ([]byte, error) {
	s := strings.TrimSpace(text)
	if s == "" {
		return nil, errors.New("empty model text")
	}
	// Markdown ```json ... ``` or ``` ... ```
	if strings.HasPrefix(s, "```") {
		s = stripMarkdownFence(s)
		s = strings.TrimSpace(s)
	}
	if strings.HasPrefix(s, "[") {
		return []byte(s), nil
	}
	// Embedded: first [...] block
	idx := strings.Index(s, "[")
	if idx == -1 {
		return nil, fmt.Errorf("no JSON array found in model text")
	}
	// find matching closing bracket (naive, good enough for model arrays)
	depth := 0
	for i := idx; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return []byte(s[idx : i+1]), nil
			}
		}
	}
	return nil, fmt.Errorf("unbalanced JSON array in model text")
}

var fenceOpen = regexp.MustCompile("(?i)^```(?:json)?\\s*")

func stripMarkdownFence(s string) string {
	s = strings.TrimSpace(s)
	s = fenceOpen.ReplaceAllString(s, "")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
