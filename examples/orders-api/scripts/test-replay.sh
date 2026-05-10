#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
cd "$ROOT"

CONFIG="examples/orders-api/bt/backendtest-failures.yaml"
ARTIFACT_DIR="examples/orders-api/bt/.bt/artifacts"

echo "=== Running expected-failure cases ==="
./bt run --config "$CONFIG" --strategy table || true

echo ""
echo "=== Checking artifact was produced ==="
ARTIFACTS=$(ls "$ARTIFACT_DIR"/*.json 2>/dev/null || true)

if [[ -z "$ARTIFACTS" ]]; then
  echo "FAIL: No artifact was produced"
  exit 1
fi

echo "Artifact found:"
echo "$ARTIFACTS"

echo ""
echo "=== Replaying most recent artifact ==="
LATEST=$(ls -t "$ARTIFACT_DIR"/*.json | head -1)

./bt replay --config examples/orders-api/bt/backendtest.yaml "$LATEST" || true

echo ""
echo "=== Replay smoke test complete ==="
