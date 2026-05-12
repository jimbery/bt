.PHONY: test lint precommit integration bt install orders-api run-orders-api run-bt-orders run-bt-orders-property run-integration-local

# All default-tagged tests under ./... (race + no test cache reuse). Excludes packages that
# require //go:build integration — those run in `make integration` with orders-api up.
test:
	go test ./... -race -count=1

# Match CI: format (gofmt + goimports) then linters. Run before every commit.
lint:
	golangci-lint fmt
	golangci-lint run

# Lint + every ./... test + orders-api smoke (bt + go test -tags integration). Used by .githooks/pre-commit.
precommit: lint test integration

# Alias: same recipe as run-integration-local (start orders-api, run bt strategies).
integration: run-integration-local

bt:
	go build -o bt ./cmd/bt

# Install bt onto your PATH (same binary as CI builds). Requires $(go env GOPATH)/bin or GOBIN on PATH.
install:
	go install ./cmd/bt

orders-api:
	go build -o orders-api ./examples/orders-api

# Terminal 1: start the example API (default PORT=8080, or override with PORT=9090).
run-orders-api:
	go run ./examples/orders-api

# Terminal 2 (with API already listening on localhost:8080): run table integration cases.
run-bt-orders: bt
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy table --exclude schema-violation-acceptance-test

# Terminal 2 (with API already listening on localhost:8080): property checks on GetHealth + ListOrders.
run-bt-orders-property: bt
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy property

# One-shot: build, start orders-api in the background, run bt smoke (validate, doctor, strategies),
# then examples/orders-api integration tests (-tags integration, same as example-bt CI), then stop the API.
run-integration-local: bt orders-api
	set -e; \
	command -v jq >/dev/null 2>&1 || { echo "error: jq is required for make integration (e.g. brew install jq)" >&2; exit 1; }; \
	./orders-api & pid=$$!; \
	trap 'kill $$pid 2>/dev/null || true' EXIT; \
	for i in $$(seq 1 30); do \
		if curl -sf http://localhost:$${PORT:-8080}/health >/dev/null; then break; fi; \
		sleep 0.2; \
	done; \
	curl -sf http://localhost:$${PORT:-8080}/health >/dev/null; \
	./bt validate --config examples/orders-api/bt/backendtest.yaml --output json | jq -e '.valid == true' >/dev/null; \
	./bt doctor --config examples/orders-api/bt/backendtest.yaml; \
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy table --exclude schema-violation-acceptance-test; \
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy property; \
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy fuzz --safety safe --fuzz-iterations 20; \
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy contract; \
	go test ./examples/orders-api/integration/... -tags integration -race -count=1; \
	go test ./examples/orders-api -tags integration -race -count=1 -run TestSchemaViolationAcceptance; \
	go test ./examples/graphql-api -tags integration -race -count=1 -run TestGQLSchemaViolationAcceptance
