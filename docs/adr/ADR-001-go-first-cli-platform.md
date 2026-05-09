# ADR-001: Go-first CLI platform

**Status:** Accepted  
**Date:** 2026-05-09

---

## Context

`bt` needs to run as a developer tool locally and as a quality gate in CI pipelines. It must be installable without a runtime dependency, work across Linux, macOS, and Windows, and distribute cleanly via GitHub Releases. The primary author is most familiar with Go among languages suitable for this type of tool.

Several languages were considered:

- **Go** — single static binary, native fuzzing support in 1.18+, strong concurrency primitives, mature CLI ecosystem (Cobra, Viper)
- **Python** — faster scripting velocity, richer ecosystem for schema parsing and LLM SDKs, but poor binary distribution story and GIL limitations for concurrent execution
- **Rust** — excellent performance and binary distribution, but significantly higher learning curve and slower initial velocity for a solo developer

## Decision

Build `bt` as a Go CLI application. Use Cobra for command structure and Viper for configuration management.

## Rationale

- **Single binary distribution** is the most important factor for a CI-first tool. Go compiles to a fully static binary with `CGO_ENABLED=0`. Python requires runtime management (virtualenv, pipx, PyInstaller); Rust is comparable but carries higher cost for a solo developer
- **Native fuzzing** (`go test -fuzz`) is a first-class toolchain feature from Go 1.18. No external dependency required
- **Concurrency model** — goroutines and `context.Context` are a natural fit for the execution engine, which needs parallel operation execution, per-endpoint throttling, and cancellation
- **Ecosystem fit** — the tools this competes with most directly (golangci-lint, migrate, k6, HashiCorp CLI suite) are all Go. This is not coincidence; Go is well suited to developer tooling
- **Cobra and Viper** are the de facto standard for Go CLI tools with subcommands, config files, environment variable overrides, and flag merging

## Consequences

- Python's more mature OpenAPI parsing and LLM SDK ecosystem will require thinner Go equivalents; acceptable because those layers arrive late in the roadmap (M6–M7)
- Config and error handling boilerplate is more verbose in Go than Python; mitigated by the compiler catching errors that would be runtime failures in Python
- GoReleaser handles cross-compilation and GitHub Release publishing with minimal configuration
- `go-version-file: go.mod` in CI ensures a single source of truth for the Go version