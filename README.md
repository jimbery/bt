# bt

`bt` is a Go-native **backend testing CLI**. It discovers operations from an **OpenAPI 3** document or a **GraphQL SDL** file, runs **table**, **property**, **fuzz**, and **contract** strategies against a live **`target.base_url`**, writes **reports** (console, JSON, JUnit), and on failure can persist **artifacts** plus a **`replay`** path for the same HTTP request.

An **MCP server** exposes the same engine to AI clients (`bt mcp serve` / `bt mcp call`).

## Requirements

- [Go](https://go.dev/dl/) **1.25+** (see `go.mod`).
- Optional: [golangci-lint](https://golangci-lint.run/welcome/install/) **v2** if you run `make lint` locally.
- Optional: `curl` and `jq` for `make run-integration-local` / `make precommit` integration steps.

## Feature list

| Feature | What it does |
|---------|----------------|
| **OpenAPI adapter** | Default: discover operations, methods, paths, and bodies from `target.schema` (YAML/JSON OpenAPI 3). |
| **GraphQL adapter** | `target.adapter: graphql` — discover query/mutation/subscription fields from SDL (`target.schema`) or introspection when only `base_url` is set. |
| **Table strategy** | YAML-driven cases: explicit HTTP (or GraphQL-over-HTTP) inputs and expectations (`status_code`, headers, JSON body / `gql_*` expectations). |
| **Property strategy** | Randomised checks per operation with named **invariants** (e.g. `no_5xx`, `response_matches_schema`). |
| **Fuzz strategy** | Mutate seed requests (payload + REST header/path/query mutators; GraphQL uses payload mutator), classify responses, optionally grow a **corpus** on interesting failures. |
| **Contract strategy** | Assert responses against the operation model derived from the spec / SDL. |
| **Run all strategies** | `--strategy all` runs table, then property, then fuzz, then contract (skips types missing from config). |
| **Safety profiles** | Fuzz respects `safety.profile` in config; override with `--safety` on `run`. |
| **Bearer auth** | `target.auth`: `type: bearer`, `env: <VAR>` — value read from process env; optional `--load-dotenv`. |
| **Reports** | `--output` / `report.formats`: `console`, `json`, `junit`; optional `--output-file`. |
| **Failure artifacts** | JSON under `<config-dir>/.bt/artifacts/` with request, response, failures (and auth diagnostics where applicable). |
| **Replay** | `bt replay <artifact.json>` re-sends the stored request and checks whether original failures still apply. |
| **Doctor** | Pre-flight checks: schema file reachable, optional `GET {base}/health`, auth env set, optional contract baseline file. |
| **Init** | Scaffold a starter `backendtest.yaml`. |
| **Validate** | Load and validate config (human text or `--output json`). |
| **MCP** | Long-lived **`bt mcp serve`** (stdio) or one-shot **`bt mcp call <tool>`** for validate, run, discover, scaffold, suggest tools, explain failure. |

## Setup (build from source)

From the repository root:

```bash
go build -o bt ./cmd/bt
```

Or:

```bash
make bt
```

Put `./bt` on your `PATH`, or invoke `./bt`. Examples below assume commands run from the **repository root** unless noted.

## Quick start (empty project)

```bash
mkdir my-api-tests && cd my-api-tests
/path/to/bt init              # creates backendtest.yaml
/path/to/bt validate
```

Edit `backendtest.yaml` with your API name, `target.base_url`, `target.schema`, and a `table` strategy pointing at a case file. Then:

```bash
/path/to/bt run --strategy table
```

---

## How to use each feature

### Global flags (root / most commands)

| Flag | Default | Purpose |
|------|---------|---------|
| `--config` | `backendtest.yaml` | Path to the bt config file |
| `--strategy` | `table` | For **`bt run`**: `table`, `property`, `fuzz`, `contract`, or **`all`** |
| `--output` | `console` | `console`, `json`, or `junit` (where supported) |
| `--adapter` | _(from config)_ | Override **`openapi`** or **`graphql`** (`target.adapter`) |
| `--env` | _(empty)_ | Optional environment label (recorded in artifacts) |

### `bt init`

Scaffolds **`backendtest.yaml`** in the current directory (or path from `--config`).

```bash
./bt init
./bt init --config ./my-project/backendtest.yaml --force   # overwrite
```

### `bt validate`

Loads **`backendtest.yaml`** and fails if the config is invalid (paths, strategies, target shape).

```bash
./bt validate --config ./backendtest.yaml
./bt validate --config ./backendtest.yaml --output json    # {"valid":true,"errors":[]} or errors list
```

### `bt run`

1. Loads config  
2. Optionally **`--load-dotenv`** — before reading bearer auth, loads **unset** keys from, in order: `<config-dir>/.env`, `<config-dir>/../.env`, `./.env` (never overrides existing env vars)  
3. Validates the adapter and **discovers operations**  
4. Builds the requested **strategy** (or each strategy when **`--strategy all`**)  
5. Executes HTTP requests via the default runner (REST JSON or GraphQL POST to `graphql_path`, default **`/graphql`**)  
6. Writes the selected **report**; on failures, may write **artifacts** under `<config-dir>/.bt/artifacts/`

**`bt run`-specific flags:**

| Flag | Purpose |
|------|---------|
| `--strategy` | `table` \| `property` \| `fuzz` \| `contract` \| **`all`** |
| `--load-dotenv` | Load `.env` files as above for secrets such as bearer tokens |
| `--seed` | Property strategy: PRNG seed (deterministic runs) |
| `--checks` | Property strategy: number of checks per operation (overrides config when set) |
| `--fuzz-iterations` | Fuzz: **maximum** HTTP attempts per operation (budget; actual attempts also depend on corpus seeds × mutators) |
| `--corpus-dir` | Fuzz: directory of seed JSON files (default `<config-dir>/corpus`) |
| `--safety` | Fuzz: override safety profile — `safe`, `aggressive`, `destructive` |
| `--output-file` | Also write the report to a file (`json`/`junit` to file only; `console` duplicates to file when set) |

Examples:

```bash
./bt run --config ./backendtest.yaml --strategy table
./bt run --config ./backendtest.yaml --strategy all --adapter graphql --load-dotenv
./bt run --config ./backendtest.yaml --strategy property --seed 42 --checks 100
./bt run --config ./backendtest.yaml --strategy fuzz --fuzz-iterations 200 --corpus-dir ./my-corpus
./bt run --config ./backendtest.yaml --output json --output-file ./.bt/reports/latest.json
```

### `bt doctor`

Runs static / light network checks: schema file exists, **`GET {base_url}/health`** returns 200 (REST-oriented smoke), bearer **auth env** non-empty when configured, optional **contract baseline** YAML parseable.

```bash
./bt doctor --config ./backendtest.yaml
./bt doctor --config ./backendtest.yaml --output json
```

GraphQL-only services often have no **`/health`**; treat doctor as **advisory** for those targets and rely on **`bt validate`** + **`bt run`**.

### `bt replay`

Loads one **artifact JSON** and re-issues the same request against **`target.base_url`** from `--config`, then checks whether the **original failure invariants** still hold on the new response.

```bash
./bt replay --config ./backendtest.yaml ./.bt/artifacts/2026-05-09T120000Z-my-case.json
```

### `bt mcp serve`

Starts the **Model Context Protocol** server on **stdio** (stdout is the MCP stream; log to stderr). AI tools (Cursor, Claude Code, etc.) register this command and call registered tools.

```bash
./bt mcp serve --config ./backendtest.yaml   # default config_path when tools omit it
```

### `bt mcp call`

Starts a **short-lived** MCP subprocess, invokes **one** tool, prints **JSON** to stdout, exits. Useful in CI and shell scripts. **`--load-dotenv` is not applied here** — export secrets or wrap the call.

```bash
./bt mcp call bt_validate --input '{"config_path":"/abs/path/backendtest.yaml"}' --output json
./bt mcp call bt_run --input '{"config_path":"/abs/path/backendtest.yaml","strategy":"table"}' --output json
```

**MCP tools** (names as exposed to clients):

| Tool | Use |
|------|-----|
| **`bt_discover_operations`** | List operations from an **OpenAPI** `schema_path` (not used for GraphQL SDL). |
| **`bt_validate`** | Same as CLI validate for a `config_path`. |
| **`bt_run`** | Same as CLI run for `config_path`, optional `strategy`, optional `seed`. |
| **`bt_explain_failure`** | Load one artifact path; returns structured request/response/failures and a suggested **`bt replay`** line. |
| **`bt_scaffold_config`** | Produce starter YAML from a schema path. |
| **`bt_suggest_strategy`** | Heuristic (or AI-backed when configured) strategy hints per operation. |
| **`bt_suggest_invariants`** | Heuristic (or AI-backed) invariant suggestions for an operation. |

---

## Configuration (`backendtest.yaml`)

Minimal shape:

```yaml
version: 1

target:
  name: my-api
  base_url: http://localhost:8080
  schema: ./openapi.yaml              # OpenAPI 3, or GraphQL SDL when adapter is graphql
  adapter: openapi                    # omit for OpenAPI; use "graphql" for GraphQL
  graphql_path: /graphql              # optional; default /graphql when adapter is graphql
  environment: local                  # optional; recorded in artifacts
  auth:
    type: bearer
    env: MY_API_TOKEN                 # name of env var holding the secret (not the secret itself)

strategies:
  - type: table
    file: ./tests/table.yaml
  - type: property
    operations: [CreateOrder]
    invariants: [no_5xx, response_matches_schema]
    config:
      checks: 50
      seed: 42
  - type: fuzz
    operations: [CreateOrder]
    config:
      fuzz_iterations: 50
      corpus_dir: ./corpus            # optional; default <config-dir>/corpus
  - type: contract
    operations: [CreateOrder]

report:
  formats: [console, json, junit]
  output_dir: ./.bt/reports

safety:
  profile: safe                         # safe | aggressive | destructive (fuzz)

# Optional: contract baseline for quarantined known gaps (see contract strategy docs)
# baseline: .bt/baseline.yaml
```

**Paths** for `schema`, table `file`, `report.output_dir`, and `baseline` are resolved from the **process working directory** unless absolute. From a nested config (e.g. `examples/orders-api/bt/`), run from repo root with paths like `examples/orders-api/spec/openapi.yaml`.

### Strategies (YAML + behaviour)

| Strategy | Config highlights | CLI overrides |
|----------|---------------------|---------------|
| **table** | `file:` points at a cases YAML | — |
| **property** | `operations:`, `invariants:`, optional `config.checks` / `seed` | `--seed`, `--checks` |
| **fuzz** | `operations:`, optional `config.fuzz_iterations`, `corpus_dir` | `--fuzz-iterations`, `--corpus-dir`, `--safety` |
| **contract** | `operations:` | Baseline optional via `baseline:` on target or default `.bt/baseline.yaml` next to config |

### Table cases (`table.yaml`)

Each case has **`id`**, optional **`operation_id`** (links to discovery), **`input`**, and optional **`expected`**.

**REST:** `method`, `path`, optional `query`, `headers`, `body`.

**GraphQL:** same POST **`path`** (usually `/graphql`), plus **`gql_query`**, optional **`gql_variables`**. Expectations may include **`gql_no_errors`**, **`gql_data`**, **`gql_data_schema`** (JSON-schema-shaped), etc., depending on your `bt` version.

**REST response body** expectations may use **`schema`** (inline JSON Schema fragment). OpenAPI **`$ref`** strings are not resolved in table `schema`; use **contract** for full spec-driven checks.

```yaml
cases:
  - id: health-returns-200
    operation_id: GetHealth
    input:
      method: GET
      path: /health
    expected:
      status_code: 200
      schema:
        type: object
        required: [status]
        properties:
          status:
            type: string
```

Run with **`./bt run --strategy table`**.

### Reports and failure artifacts

- **`--output`** on **`bt run`**: `console`, `json`, `junit`.
- Failed cases may write **JSON artifacts** under **`<config-dir>/.bt/artifacts/`**. The console report can include an **`artifact:`** path and a suggested **`bt replay`** command.
- **`bt replay`** re-runs the request and evaluates whether recorded failures still apply.

---

## Example: in-repo `orders-api`

The repository includes a small HTTP API and a full bt setup under **`examples/orders-api/`**.

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

Deliberately failing cases live in **`examples/orders-api/bt/backendtest-failures.yaml`** and **`examples/orders-api/bt/cases/table-expected-failures.yaml`**. With the API running:

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

---

## Development

For **code changes** (Go, CI, or executable scripts), branch from **`main`**, push that branch, and merge via PR. **Documentation-only** changes (README, milestones, ADRs) may go straight on `main` when small; use a branch if the edit is large or you want review on its own.

Before you commit, run the same checks as CI (formatters plus linters):

```bash
make lint
```

Or run **lint, race tests, and local integration** together (same as the git hook):

```bash
make precommit
```

That runs `golangci-lint fmt`, `golangci-lint run`, **`go test ./... -race -count=1`**, then **`make integration`**: builds `bt` and `orders-api`, starts the example API briefly, runs **`bt`** validate, doctor, **table**, **property**, **fuzz**, and **contract**, then **`go test ./examples/orders-api/integration/... -tags integration -race -count=1`**. Install [golangci-lint](https://golangci-lint.run/welcome/install/) v2 locally. **Requires `curl`** and **`jq`** on your PATH.

**Optional — run `make precommit` on every commit:**

```bash
git config core.hooksPath .githooks
chmod +x .githooks/pre-commit   # once per clone
```

The hook runs **`make precommit`**; fix failures before `git commit` succeeds. Integration needs a free **`PORT`** (default **8080**).

## Module path

This repository uses **`github.com/jayimbery/bt`**. Replace it with your org’s import path if you fork.
