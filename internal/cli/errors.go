package cli

import "errors"

// ErrTestFailures is returned when one or more cases failed (exit code 1).
var ErrTestFailures = errors.New("one or more test cases failed")
