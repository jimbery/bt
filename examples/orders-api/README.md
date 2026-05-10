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

OpenAPI spec: `openapi.yaml`. Table cases: `bt/tests/table.yaml`.
