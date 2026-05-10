package contract

// ViolationSeverity classifies a contract violation's impact.
type ViolationSeverity int

const (
	// Critical violations cause the contract case to fail.
	Critical ViolationSeverity = iota
	// Warning violations are recorded but do not fail the case.
	Warning
)

func (s ViolationSeverity) String() string {
	switch s {
	case Critical:
		return "critical"
	case Warning:
		return "warning"
	default:
		return "unknown"
	}
}

// ContractViolation is a single disagreement between a response and its schema.
type ContractViolation struct {
	Field    string // JSON path, e.g. "order.status" or "tags[1]"
	Expected string // human description of what the schema declares
	Actual   string // what was found in the response
	Severity ViolationSeverity
}
