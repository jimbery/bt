package exitcode_test

import (
	"errors"
	"testing"

	"github.com/jayimbery/bt/internal/exitcode"
	"github.com/jayimbery/bt/internal/strategy/contract"
)

func TestExitCode_AllPassed_Zero(t *testing.T) {
	results := []contract.AnnotatedResult{
		{ContractResult: contract.ContractResult{Passed: true}},
	}
	code := exitcode.FromContractResults(results, nil)
	if code != 0 {
		t.Errorf("expected 0, got %d", code)
	}
}

func TestExitCode_OneFailed_One(t *testing.T) {
	results := []contract.AnnotatedResult{
		{ContractResult: contract.ContractResult{Passed: false}},
	}
	code := exitcode.FromContractResults(results, nil)
	if code != 1 {
		t.Errorf("expected 1, got %d", code)
	}
}

func TestExitCode_QuarantinedFailure_Zero(t *testing.T) {
	results := []contract.AnnotatedResult{
		{
			ContractResult: contract.ContractResult{Passed: false},
			Quarantined:    true,
		},
	}
	code := exitcode.FromContractResults(results, nil)
	if code != 0 {
		t.Errorf("quarantined failure must not affect exit code, got %d", code)
	}
}

func TestExitCode_ConfigError_Two(t *testing.T) {
	code := exitcode.FromContractResults(nil, exitcode.ErrConfig{Err: errors.New("bad config")})
	if code != 2 {
		t.Errorf("expected 2 for config error, got %d", code)
	}
}

func TestExitCode_ExecutionError_Three(t *testing.T) {
	code := exitcode.FromContractResults(nil, exitcode.ErrExecution{Err: errors.New("network down")})
	if code != 3 {
		t.Errorf("expected 3 for execution error, got %d", code)
	}
}
