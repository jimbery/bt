package exitcode

import (
	"errors"

	"github.com/jayimbery/bt/internal/strategy/contract"
)

const (
	CodeOK             = 0
	CodeTestFailures   = 1
	CodeConfigError    = 2
	CodeExecutionError = 3
)

// ErrConfig indicates a configuration or schema problem (exit code CodeConfigError).
type ErrConfig struct{ Err error }

func (e ErrConfig) Error() string { return e.Err.Error() }
func (e ErrConfig) Unwrap() error { return e.Err }

// ErrExecution indicates a runtime execution problem such as network failure (exit code CodeExecutionError).
type ErrExecution struct{ Err error }

func (e ErrExecution) Error() string { return e.Err.Error() }
func (e ErrExecution) Unwrap() error { return e.Err }

// WrapConfig wraps err as a config-layer error for exit-code mapping.
func WrapConfig(err error) error {
	if err == nil {
		return nil
	}
	return ErrConfig{Err: err}
}

// WrapExecution wraps err as an execution-layer error for exit-code mapping.
func WrapExecution(err error) error {
	if err == nil {
		return nil
	}
	return ErrExecution{Err: err}
}

// FromContractResults maps annotated contract results and an outer error to a stable exit code.
func FromContractResults(results []contract.AnnotatedResult, err error) int {
	if err != nil {
		var ec ErrConfig
		if errors.As(err, &ec) {
			return CodeConfigError
		}
		var ee ErrExecution
		if errors.As(err, &ee) {
			return CodeExecutionError
		}
		return CodeExecutionError
	}
	for _, r := range results {
		if r.Failed() {
			return CodeTestFailures
		}
	}
	return CodeOK
}
