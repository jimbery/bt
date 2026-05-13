package tests_test

import "testing"

// The GraphQL example bt integration tests use //go:build integration; this file keeps
// `go test ./...` working without requiring the integration tag for an empty package.
func TestPackage_Compiles(t *testing.T) {
	t.Parallel()
}
