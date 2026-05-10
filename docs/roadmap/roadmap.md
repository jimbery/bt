# bt — Backend Testing Platform Roadmap

## Overview

This roadmap covers the full development plan for `bt`, a Go-native backend testing platform that unifies table-driven, property-based, fuzz, and contract testing strategies under a single CLI and MCP-compatible interface.

The roadmap is organised into eight milestones. Each milestone has a clear exit criterion — a concrete thing that works — before the next begins. Items in the deferred bucket are genuine planned work, not abandoned ideas.

### How milestones, playbooks, and ADRs fit together

| Layer | Role |
|-------|------|
| **ADRs** (`/docs/adr/`) | Enduring architecture decisions (for example ADR-002’s shared `Strategy` contract and target lifecycle). They describe the shape of the system over time, not “done when M2 ships.” |
| **This roadmap** | What each milestone is *for* and how it maps to those ADRs. Exit criteria are product-level outcomes. |
| **Milestone playbooks** (`/docs/milestones/`) | Step-by-step build guides. When a playbook is narrower than a roadmap bullet, **the playbook wins for scope** until the roadmap is updated here. |

### Implementation progress (living)

Update this table when a milestone’s exit criterion is met or when scope intentionally shifts. *Status* is against this repo’s `main` line, not the original week estimates.

| Milestone | Playbook | Primary ADRs | Status | Notes |
|-----------|----------|--------------|--------|--------|
| M1 | [m1-foundation.md](../milestones/m1-foundation.md) | ADR-001 | **Met** | CLI scaffold, config load/validate, domain model, CI. |
| M2 | [m2-open-api-adapter-strat.md](../milestones/m2-open-api-adapter-strat.md) | ADR-001, ADR-002, ADR-006 | **Partial** | OpenAPI adapter, HTTP runner, table strategy (YAML cases), console/JSON/JUnit reports, `bt run --strategy table`. ADR-002: `Strategy` + shared types in use; full `internal/engine` extraction and config-driven invariants not done. Assertions match playbook: status + headers only; response body schema checks deferred (see M4). Per-operation throttling not shipped yet (`internal/safety`, `internal/engine/scheduler` are stubs). |
| M3 | [m3-replay-artifact-model.md](../milestones/m3-replay-artifact-model.md) | ADR-002 | **Partial** | Replay load/write, `bt replay`, failure artifacts from table runs; confirm full bundle fields and exit criterion against the M3 playbook. |
| M4 | [m4-property-strategy.md](../milestones/m4-property-strategy.md) | ADR-002, ADR-003 | **Partial** | `bt run --strategy property` with Rapid, schema-derived request body generation, `no_5xx` / `response_matches_schema` / idempotency invariants, `--seed` / `--checks`, failure artifacts. Remaining vs playbook: richer reporting, broader schema/generator coverage, full replay parity checks. |
| M5 | — | ADR-001, ADR-002, ADR-004 | Not started | Fuzz strategy, safety profiles, throttling/rate limits as in ADR-001. |
| M6 | — | ADR-007 | Not started | MCP server surface (`bt mcp serve`). |
| M7 | — | ADR-007 | Not started | AI scaffold / suggestions; engine determinism unchanged. |
| M8 | — | ADR-002, ADR-006 | Not started | Contract strategy, CI hardening; ADR-005 remains deferred. |

---

## Milestones

### M1 — Foundation
**Weeks 1–4**

The structural skeleton of the project. Nothing generative yet, just a working CLI that can read config and validate it.

Deliverables:
- Monorepo scaffold (`/cmd`, `/internal`, `/pkg`, `/docs/adr`)
- GitHub Actions CI workflow (lint, test with `-race`, build)
- GoReleaser setup for binary releases
- Cobra root command and subcommand stubs (`init`, `validate`, `run`, `replay`, `doctor`)
- Viper config loading and schema validation
- Core domain model types (`Target`, `Operation`, `Invariant`, `StrategySpec`, `TestPlan`, `Case`, `Result`)
- Logging and structured error model
- ADR-001, ADR-002, ADR-003, ADR-006 **Accepted**; ADR-005 **Deferred**; ADR-004, ADR-007 **Proposed** (see [Architecture Decision Records](#architecture-decision-records) below)

Exit criterion: `bt init` scaffolds a valid `backendtest.yaml`, `bt validate` checks it against schema and reports errors clearly.

---

### M2 — OpenAPI Adapter + Table Strategy
**Weeks 5–9**

The first end-to-end path. A team can write deterministic table tests against a real API and run them from the CLI.

Deliverables:
- OpenAPI discovery adapter (`/internal/adapter/openapi`) — **ADR-006**
- HTTP runner with `context.Context` cancellation and request timeout — **ADR-001**
- Table strategy: case files in YAML (see playbook); JSON/CSV loaders remain optional follow-ups
- Table assertions: expected **status code** and **response headers** (aligned with [m2 playbook](../milestones/m2-open-api-adapter-strat.md) Step 4). OpenAPI **response schemas** are normalised on `Operation` for later strategies; **body/schema invariant evaluation** is **M4** (ADR-002 lifecycle step 4 in full), not required for M2 exit
- **ADR-002**: shared `Strategy` / `Executor` contract and `Case` / `Result` model in the run path; extraction of orchestration into `internal/engine` can trail the exit criterion
- JSON and JUnit report outputs; console summary renderer

Exit criterion: A team can write deterministic backend tests against a real API and run them with `bt run --strategy table`.

---

### M3 — Replay + Artifact Model
**Weeks 10–12**

Replay infrastructure built before any generative strategy, so all later strategies inherit it automatically (**ADR-002**: one artifact/replay path for every strategy).

Deliverables:
- Replay bundle writer (`/internal/replay`)
- Artifact directory structure (`.bt/artifacts/`)
- `bt replay <artifact-path>` command
- Bundle contents: strategy kind, seed, operation detail, request/response payloads, assertion failures, environment metadata
- Gitignore defaults for `.bt/`

Exit criterion: Any test failure produces a portable artifact bundle that can be replayed exactly with one command on any machine.

---

### M4 — Property Strategy
**Weeks 13–19**

The core generative testing capability. The tool should find real bugs and shrink them to minimal reproducible cases (**ADR-003**; **ADR-002** shrinking / minimisation in the shared lifecycle where applicable).

Deliverables:
- Rapid integration (`/internal/strategy/property`)
- Schema-derived input generators from normalised OpenAPI operations
- Shrinking support with minimised failure output
- Built-in invariants: `no_5xx`, `response_matches_schema`, `idempotency_key_prevents_duplicates`
- Seed and replay metadata stored in artifact bundles
- `--seed` flag for deterministic reruns

Exit criterion: The tool finds real bugs in a target API and reproduces them deterministically from a seed.

---

### M5 — Fuzz Mode + Safety Model
**Weeks 20–25**

Mutation-heavy robustness testing using native Go fuzzing. Safety model designed in from the start (**ADR-004**; **ADR-001** concurrency/throttling for execution).

Deliverables:
- Native Go fuzzing integration (`/internal/strategy/fuzz`)
- Payload, header, path, and query string mutators
- Safety profiles: `safe`, `aggressive`, `destructive`
- Method allow/deny lists
- Per-operation throttling and rate limits
- Corpus management (seed from prior failures or property-generated cases)
- Failure classifications: crash, timeout, validation leak, schema break
- Environment profiles: local, CI, staging, preprod
- Explicit opt-in required for destructive modes

Exit criterion: Fuzz mode runs safely in CI without manual guards, and produces classified failure reports.

---

### M6 — MCP Server
**Weeks 26–30**

First-class MCP interface. Any MCP-compatible client (Claude, Cursor, Zed, etc.) can orchestrate the platform (**ADR-007**).

Deliverables:
- `bt mcp serve` subcommand (long-running MCP protocol server)
- Tool registry and dispatch (`/internal/mcp`)
- JSON Schema definitions for all tool inputs and outputs
- Initial tool surface:
  - `bt_discover_operations` — given a schema path, return structured operations
  - `bt_suggest_strategy` — given operations, recommend strategy modes
  - `bt_scaffold_config` — generate `backendtest.yaml` from recommendations
  - `bt_validate` — validate a config before running
  - `bt_run` — execute a plan, return structured summary + artifact path
  - `bt_explain_failure` — given an artifact path, return structured failure detail
- Structured exit codes: 0 = pass, 1 = test failures, 2 = config/execution error
- Artifact path as shared state between tool calls (avoids oversized inline responses)

Exit criterion: Any MCP-compatible client can invoke `bt_run` against a real API and receive structured results.

---

### M7 — AI Scaffold Layer
**Weeks 31–35**

AI as a guide and scaffolder, not a runtime participant. Determinism of the execution engine is unaffected (**ADR-007**).

Deliverables:
- `bt_suggest_invariants` MCP tool — given an operation, propose invariant candidates for review
- Strategy recommendation logic — maps operation shape to appropriate strategy modes
- Template generation — produces starter `backendtest.yaml` and table test files from schema
- `AIProvider` interface with one external implementation (Anthropic/OpenAI API call)
- Clean opt-out: all functionality works identically without the AI layer
- Schema and artifact models confirmed compatible with AI context requirements

Exit criterion: Pointing an MCP client at a schema produces a useful starter config and a set of invariant suggestions for review.

---

### M8 — Contract Strategy + CI Hardening
**Weeks 36–40**

Contract verification as a strategy mode. The tool can sit in a real CI pipeline as a quality gate.

Deliverables:
- Contract strategy (`/internal/strategy/contract`)
- Provider behaviour verification against schema
- CI profiles with appropriate defaults
- Baseline and quarantine handling for flaky or expected failures
- Improved exit codes and gating behaviour
- GitHub Actions install snippet and example workflow
- `bt doctor` command for environment and config diagnosis

Exit criterion: The tool runs in a real CI pipeline, gates on failures, and does not produce excessive noise.

---

## Deferred

The following are planned work but not blockers for a useful v1. They slot in after M8.

- **Graph/state strategy** — multi-step workflow testing, resource dependency tracking, lifecycle invariants
- **Plugin SDK** — external strategy and adapter plugins via `go-plugin` over gRPC
- **Additional protocol adapters** — GraphQL, gRPC (proto schema)
- **Trace adapter** — production-traffic-derived test seeds
- **Rich web UI** — HTML reports, trend comparison, operation heatmaps
- **Enterprise layer** — RBAC, centralised artifact storage, audit logging, multi-target orchestration

---

## Architecture Decision Records

| ADR | Title | Status | Primary milestones |
|-----|-------|--------|---------------------|
| ADR-001 | Go-first CLI platform | Accepted | M1–M2 (CLI, runner); M5 (fuzz toolchain) |
| ADR-002 | Unified strategy engine | Accepted | M1 (types); M2+ (contract); M3 (replay parity); full engine extraction rolls forward with milestones |
| ADR-003 | Rapid for property testing | Accepted | M4 |
| ADR-004 | Native Go fuzzing for robustness mode | Proposed | M5 |
| ADR-005 | RPC/gRPC plugin boundary | Deferred | Post–M8 / deferred bucket |
| ADR-006 | OpenAPI-first adapter strategy | Accepted | M2, M8 |
| ADR-007 | MCP-first AI integration | Proposed | M6–M7 |

ADRs live in `/docs/adr/`. **Accepted** means the decision is in force in the codebase (possibly only partially implemented — see *Implementation progress* above). **Proposed** is awaiting the milestone that implements it; **Deferred** matches post–M8 / deferred roadmap work.

---

## Key Design Constraints

- `internal/engine` must not import `internal/cli` or `internal/mcp` — keeps extraction from the current `bt run` orchestration in `internal/cli/run.go` safe (see `internal/engine/README.md` and **ADR-002**)
- CLI and MCP server are both entry points into the same execution path; long term, shared orchestration belongs in `internal/engine`, not duplicated in each surface (**ADR-002**)
- Every randomised run is seedable, replayable, and debuggable (**ADR-002**, **ADR-003**, **ADR-004**)
- Destructive and fuzz-heavy modes require explicit opt-in (**ADR-004**)
- The AI layer only touches the schema/operation model and config/template model — it never replaces the engine, executor, or reporter (**ADR-007**)
- Single static binary, CGO disabled, distributable via GitHub Releases (**ADR-001**)

---

## Distribution

Releases are published to GitHub Releases via GoReleaser on tag push. Install in CI:

```bash
curl -sSL https://github.com/yourorg/bt/releases/latest/download/bt_linux_amd64.tar.gz | tar -xz
sudo mv bt /usr/local/bin/
```

Config file conventions:
- `backendtest.yaml` — project config, committed to repo
- `.bt/` — runtime artifacts and replays, gitignored
- `~/.config/bt/config.yaml` — global config and API keys