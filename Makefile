.PHONY: test lint precommit bt orders-api run-orders-api run-bt-orders run-bt-orders-property run-integration-local

test:
	go test ./... -race

# Match CI: format (gofmt + goimports) then linters. Run before every commit.
lint:
	golangci-lint fmt
	golangci-lint run

# Lint + race tests (used by .githooks/pre-commit).
precommit: lint test

bt:
	go build -o bt ./cmd/bt

orders-api:
	go build -o orders-api ./examples/orders-api

# Terminal 1: start the example API (default PORT=8080, or override with PORT=9090).
run-orders-api:
	go run ./examples/orders-api

# Terminal 2 (with API already listening on localhost:8080): run table integration cases.
run-bt-orders: bt
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy table

# Terminal 2 (with API already listening on localhost:8080): property checks on GetHealth + ListOrders.
run-bt-orders-property: bt
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy property

# One-shot: build, start orders-api in the background, run bt, then stop the API.
run-integration-local: bt orders-api
	set -e; \
	./orders-api & pid=$$!; \
	trap 'kill $$pid 2>/dev/null || true' EXIT; \
	for i in $$(seq 1 30); do \
		if curl -sf http://localhost:$${PORT:-8080}/health >/dev/null; then break; fi; \
		sleep 0.2; \
	done; \
	curl -sf http://localhost:$${PORT:-8080}/health >/dev/null; \
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy table; \
	./bt run --config examples/orders-api/bt/backendtest.yaml --strategy property
