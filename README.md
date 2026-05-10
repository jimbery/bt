# bt

`bt` is a Go-native backend testing CLI. It discovers operations from an **OpenAPI** description, runs **table** tests you define in YAML against a live **base URL**, and can write **failure artifacts** plus a **`replay`** command to re-run a captured request.

Today the supported strategy in the CLI is **table**; property, fuzz, and contract strategies are planned.

## Requirements

- [Go](https://go.dev/dl/) **1.25+** (see `go.mod`).
- Optional: [golangci-lint](https://golangci-lint.run/welcome/install/) **v2** if you run `make lint` locally.
- Optional: `curl` for `make run-integration-local`.

## Setup (build from source)

From the repository root:

```bash
go build -o bt ./cmd/bt
```

Or use Make:

```bash
make bt
```

Put `./bt` on your `PATH`, or invoke it as `./bt`. All examples below assume you run commands from the **repository root** unless noted.

## Quick start (empty project)

```bash
mkdir my-api-tests && cd my-api-tests
/path/to/bt init              # creates backendtest.yaml
/path/to/bt validate
```

Edit `backendtest.yaml` with your API name, `target.base_url`, path to `target.schema` (OpenAPI 3 file), and a table strategy pointing at a case file. Then:

```bash
/path/to/bt run --strategy table
```

## How to use `bt`

### Global flags

| Flag | Default | Purpose |
|------|---------|---------|
| `--config` | `backendtest.yaml` | Path to the bt config file |
| `--strategy` | `table` | Strategy to run (`run` only) |
| `--output` | `console` | Report format: `console`, `json`, or `junit` |
| `--env` | _(empty)_ | Reserved for environment profiles |

### Commands

| Command | What it does |
|---------|----------------|
| `init` | Writes a starter `backendtest.yaml` (use `--force` to overwrite) |
| `validate` | Loads and validates the config file |
| `run` | Discovers operations from the OpenAPI schema, plans table cases, executes HTTP requests, prints results |
| `replay <artifact.json>` | Loads a failure artifact and sends the same request again against `target.base_url` from `--config` |
| `doctor` | Placeholder health check (no-op for now) |

Examples:

```bash
./bt validate --config ./backendtest.yaml
./bt run --config ./backendtest.yaml --strategy table
./bt run --config ./backendtest.yaml --output json
./bt replay --config ./backendtest.yaml ./.bt/artifacts/2026-05-09T120000Z-my-case.json
```

### Configuration (`backendtest.yaml`)

Minimal shape:

```yaml
version: 1

target:
  name: my-api
  base_url: http://localhost:8080
  schema: ./openapi.yaml          # OpenAPI 3 document
  environment: local              # optional; recorded in failure artifacts

strategies:
  - type: table
    file: ./tests/table.yaml      # table case file

report:
  formats: [console, json, junit]
  output_dir: ./.bt/reports      # convention in examples; bt run uses --output for format

safety:
  profile: safe
```

**Paths** in the config (schema, case `file`, `report.output_dir`) are resolved relative to the **process working directory**, not the config file’s directory. When using nested configs (for example under `examples/orders-api/bt/`), run `bt` from the repo root and use paths like `examples/orders-api/spec/openapi.yaml`, matching CI.

### Table tests (`table.yaml`)

Cases are a list of `id`, optional `operation_id` (for discovery alignment with OpenAPI), `input` (HTTP method, path, optional `query`, `headers`, `body`), and optional `expected` (`status_code`, `headers`).

```yaml
cases:
  - id: health-returns-200
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
```

Run them with `bt run --strategy table`.

### Reports and failure artifacts

- **Console / JSON / JUnit** output is selected with **`--output`** on `bt run` (`console`, `json`, `junit`).
- On **failed** table cases, `bt` may write a **JSON artifact** under `<directory of config>/.bt/artifacts/`. The console report prints `artifact:` and a suggested `bt replay …` line.
- **`bt replay`** re-evaluates recorded failures against the new response. Today **`status_code`** and **`response_header`** invariants from the artifact are replayed.

## Example: in-repo `orders-api`

The repository includes a small HTTP API and a full bt setup under `examples/orders-api/`.

**Terminal 1 — API:**

```bash
make run-orders-api
# or: go run ./examples/orders-api
# default: http://localhost:8080
```

**Terminal 2 — tests (from repo root):**

```bash
make run-bt-orders
# or property: make run-bt-orders-property
# equivalent:
# ./bt run --config examples/orders-api/bt/backendtest.yaml --strategy table
# ./bt run --config examples/orders-api/bt/backendtest.yaml --strategy property
```

**One-shot integration** (builds, starts API in the background, runs passing table tests, tears down):

```bash
make run-integration-local
```

### Expected failures and replay smoke (M3.5)

Deliberately failing cases live in `examples/orders-api/bt/backendtest-failures.yaml` and `examples/orders-api/bt/cases/table-expected-failures.yaml`. With the API running:

```bash
./bt run --config examples/orders-api/bt/backendtest-failures.yaml --strategy table || true
ls examples/orders-api/bt/.bt/artifacts/
LATEST=$(ls -t examples/orders-api/bt/.bt/artifacts/*.json | head -1)
./bt replay --config examples/orders-api/bt/backendtest.yaml "$LATEST"
```

Or run the bundled script (from repo root, API already up):

```bash
./examples/orders-api/scripts/test-replay.sh
```

## Development

For **code changes** (Go, CI, or executable scripts), branch from **`main`**, push that branch, and merge via PR. **Documentation-only** changes (README, milestones, ADRs) may go straight on `main` when small; use a branch if the edit is large or you want review on its own.

Before you commit, run the same checks as CI (formatters plus linters):

```bash
make lint
```

Or run **lint and race tests** together (same as the git hook):

```bash
make precommit
```

That runs `golangci-lint fmt` (gofmt + goimports) and `golangci-lint run`. **`make precommit`** adds **`go test ./... -race`**. Install [golangci-lint](https://golangci-lint.run/welcome/install/) v2 locally, or rely on CI after pushing.

**Optional — run `make precommit` on every commit** (lint + race tests, same as CI `make test` plus `make lint`):

```bash
git config core.hooksPath .githooks
chmod +x .githooks/pre-commit   # once per clone
```

The hook runs **`make precommit`**; fix failures before `git commit` succeeds.

```bash
make bt
```

## Module path

This repository uses `github.com/jayimbery/bt`. Replace it with your org’s import path if you fork.
