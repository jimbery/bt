package cli

import "errors"

// ErrTestFailures is returned when one or more cases failed (exit code 1).
var ErrTestFailures = errors.New("one or more test cases failed")

// ErrReplayFailurePresent is returned when bt replay finds the original failure still reproducible.
var ErrReplayFailurePresent = errors.New("failure still present")
