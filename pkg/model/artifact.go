package model

import "time"

type Artifact struct {
	ID           string         `json:"id"`
	StrategyKind string         `json:"strategy_kind"`
	Seed         int64          `json:"seed,omitempty"`
	CaseID       string         `json:"case_id"`
	OccurredAt   time.Time      `json:"occurred_at"`
	Environment  string         `json:"environment"`
	Request      RequestDetail  `json:"request"`
	Response     ResponseDetail `json:"response"`
	Failures     []Failure      `json:"failures,omitempty"`
	// Expected is set for table-strategy artifacts when the case had expectations (used by replay for response_matches_schema).
	Expected    *CaseExpectation `json:"expected,omitempty"`
	ShrinkTrace []string         `json:"shrink_trace,omitempty"`
	// AuthEnvName, when set, names the process env var from target.auth.env.
	// AuthEnvSetInProcess is true if that variable was non-empty when the case ran.
	AuthEnvName         string `json:"auth_env_name,omitempty"`
	AuthEnvSetInProcess bool   `json:"auth_env_set_in_process,omitempty"`
}
