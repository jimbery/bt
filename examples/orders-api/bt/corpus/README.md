# Fuzz corpus (M5)

Place seed JSON files here (`*.json`). Each file must decode as a [`mutate.Input`](../../../internal/strategy/fuzz/mutate/mutate.go) (method, path, query, headers, body).

Interesting inputs discovered during a fuzz run are written here as `<sha256>.json` files.

This directory is the default corpus location for:

`./bt run --config examples/orders-api/bt/backendtest.yaml --strategy fuzz`
