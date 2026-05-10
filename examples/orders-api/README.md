# orders-api example

Sample HTTP API used for local development and `bt` integration tests.

## Run the server

From this directory:

```bash
go run .
```

The server listens on **`:8080`** by default (see `main.go`). The bundled `bt` configs use `http://localhost:8080` as `target.base_url`.

## `bt` configs (from repository root)

**Table strategy** (deterministic YAML cases):

```bash
./bt run --config examples/orders-api/bt/backendtest.yaml --strategy table
```

**Property strategy** (Rapid generative checks on `GetHealth` and `ListOrders`):

```bash
./bt run --config examples/orders-api/bt/backendtest.yaml --strategy property
```

Optional flags:

```bash
./bt run --config examples/orders-api/bt/backendtest.yaml --strategy property --seed 42 --checks 50
```

`response_matches_schema` is evaluated when the OpenAPI response defines a schema for the status code, even if you do not list it under `invariants`.

**Expected-failure table** (for replay / artifact smoke):

```bash
./bt run --config examples/orders-api/bt/backendtest-failures.yaml --strategy table || true
```

## Layout

| Path | Purpose |
|------|---------|
| `main.go`, `handlers.go`, `store.go`, … | Example HTTP server (`package main`) |
| `spec/openapi.yaml` | OpenAPI contract; `go test ./examples/orders-api/spec` runs phrase-level checks |
| `bt/` | `backendtest*.yaml` configs and `bt/cases/*.yaml` table fixtures |
| `integration/` | Opt-in tests (`//go:build integration`) that shell out to `bt` against a running API |
| `scripts/` | Helper shell scripts (replay smoke, property verification) — run from repo root |

OpenAPI: `spec/openapi.yaml`. Table cases: `bt/cases/table.yaml`.
