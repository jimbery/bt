package cli

import (
	"errors"

	"github.com/jayimbery/bt/internal/exitcode"
	"github.com/jayimbery/bt/internal/replay"
)

// ExitCodeFor maps errors from bt commands to stable process exit codes.
func ExitCodeFor(err error) int {
	if err == nil {
		return exitcode.CodeOK
	}
	if errors.Is(err, ErrTestFailures) || errors.Is(err, ErrReplayFailurePresent) {
		return exitcode.CodeTestFailures
	}
	if errors.Is(err, replay.ErrArtifactNotFound) {
		return exitcode.CodeConfigError
	}
	var ec exitcode.ErrConfig
	if errors.As(err, &ec) {
		return exitcode.CodeConfigError
	}
	var ee exitcode.ErrExecution
	if errors.As(err, &ee) {
		return exitcode.CodeExecutionError
	}
	// Default: treat unknown errors as configuration/usage problems (historical behaviour).
	return exitcode.CodeConfigError
}
