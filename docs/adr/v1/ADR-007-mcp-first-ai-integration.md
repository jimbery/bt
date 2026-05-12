# ADR-007: MCP-first AI integration

**Status:** Proposed  
**Date:** 2026-05-09

---

## Context

`bt` should provide AI-assisted guidance — strategy recommendations, invariant suggestions, and config scaffolding — without making AI a runtime dependency or compromising the deterministic execution model. A decision is needed on how the AI layer integrates with the platform and which interface it exposes.

Options considered:

- **Embedded AI in CLI commands** — AI calls happen inline during `bt run` or `bt validate`, suggestions appear in console output. Mixes concerns, adds latency to the hot path, and ties AI behaviour to specific CLI commands
- **Separate AI daemon or sidecar** — AI runs as a separate process the CLI calls. Adds operational complexity for a solo-developed tool
- **AI as a post-run analysis step** — AI only sees completed artifacts and produces summaries. Limits usefulness to retrospective analysis
- **MCP server exposing platform capabilities as tools** — the CLI runs a long-lived MCP protocol server (`bt mcp serve`). An AI client (Claude, Cursor, Zed, or any MCP-compatible client) orchestrates the platform by calling tools. AI is the client, not embedded in the server

## Decision

Expose `bt` capabilities as an MCP (Model Context Protocol) server. AI integration is client-side and vendor-agnostic. The platform itself remains deterministic and AI-free on the execution path.

The MCP server is a subcommand of the existing binary:

```bash
bt mcp serve
```

The AI scaffold layer (M7) provides advisory tools that operate on schemas and configs, not on live execution:

- `bt_discover_operations` — parse a schema, return structured operations
- `bt_suggest_strategy` — given operations, recommend strategy modes with rationale
- `bt_scaffold_config` — generate a starter `backendtest.yaml` from schema and recommendations
- `bt_validate` — validate a config before running
- `bt_run` — execute a plan, return structured summary and artifact path
- `bt_explain_failure` — given an artifact path, return structured failure explanation
- `bt_suggest_invariants` — given an operation, propose invariant candidates for human review

## Rationale

- **Determinism preserved** — AI never touches the execution hot path. `bt run` is identical with or without an MCP client connected. The platform's core guarantee (seedable, replayable, deterministic) is unaffected
- **Vendor agnostic** — MCP is an open protocol. The platform works with Claude, Cursor, Zed, or any future MCP-compatible client without code changes. No vendor lock-in
- **Clean separation of concerns** — the CLI is the execution interface; the MCP server is the integration interface; the AI client is the orchestration layer. Each can evolve independently
- **AI as guide, not author** — the AI suggests strategies and invariants; a human reviews and owns the resulting config. Generated files are artifacts the user edits, not runtime decisions the platform makes
- **Shared engine** — CLI commands and MCP tools are both entry points into the same internal engine. No business logic is duplicated

## Consequences

- MCP tool responses must be structured JSON with well-defined schemas. Human-readable prose output from CLI commands must be separated from the machine-readable output the MCP layer uses. All commands should support a `--output json` flag or equivalent
- MCP tool results have size constraints in some clients. `bt_run` should return a summary and artifact path rather than full inline results; `bt_explain_failure` takes the artifact path and returns detail. The artifact filesystem is the shared state between tool calls
- The `AIProvider` interface must be defined such that a local model can be substituted for an external API call. This protects users whose schemas are sensitive and cannot leave their environment
- Tool descriptions must be precise and unambiguous — any MCP-compatible model must be able to select the correct tool from the description alone, without relying on model-specific prompt engineering
- The MCP server and CLI share config loading and the artifact directory. `bt mcp serve` should respect the same `backendtest.yaml` and `.bt/` conventions as `bt run`