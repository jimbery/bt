#!/usr/bin/env bash
# Verifies that bt property testing finds the broken endpoint's schema violation.
# Expects the orders API to be running on localhost:8080.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../../.." && pwd)"
ARTIFACT_DIR="$ROOT/examples/orders-api/bt/.bt/artifacts"
REPORT_FILE="$ROOT/examples/orders-api/bt/.bt/reports/property-run.json"

echo "=== Running property tests ==="
mkdir -p "$(dirname "$REPORT_FILE")"
cd "$ROOT"
"$ROOT/bt" run \
  --config examples/orders-api/bt/backendtest-property.yaml \
  --strategy property \
  --output json \
  --output-file "$REPORT_FILE" \
  || true

echo ""
echo "=== Checking report for response_matches_schema failure ==="
if ! command -v jq &>/dev/null; then
  echo "jq not available — skipping structured report check"
  exit 0
fi

SCHEMA_FAILURES=$(jq '[.results[] | select(.failures[]?.invariant == "response_matches_schema")] | length' "$REPORT_FILE" 2>/dev/null || echo "0")
if [ "$SCHEMA_FAILURES" -eq 0 ]; then
  echo "FAIL: property testing did not find a response_matches_schema failure on the broken endpoint"
  exit 1
fi
echo "OK: found $SCHEMA_FAILURES result(s) with response_matches_schema violations"

echo ""
echo "=== Checking violation paths are meaningful ==="
PATHS=$(jq -r '[.results[].failures[]? | select(.invariant == "response_matches_schema") | .path] | unique | .[]' "$REPORT_FILE" 2>/dev/null || true)
if [ -z "$PATHS" ]; then
  echo "FAIL: schema violations have no path information"
  exit 1
fi
echo "Violation paths found:"
echo "$PATHS"

echo ""
echo "=== Checking artifact was written ==="
ARTIFACT_COUNT=$(find "$ARTIFACT_DIR" -maxdepth 1 -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
if [ "${ARTIFACT_COUNT:-0}" -eq 0 ]; then
  echo "FAIL: no artifact written for property test failure"
  exit 1
fi
echo "OK: $ARTIFACT_COUNT artifact(s) found"

echo ""
echo "=== Checking seed is present in report ==="
SEED=$(jq '.results[] | select(.strategy_kind == "property") | .seed' "$REPORT_FILE" 2>/dev/null | head -1 || echo "")
if [ -z "$SEED" ] || [ "$SEED" = "null" ]; then
  echo "FAIL: seed not found in property run report"
  exit 1
fi
echo "OK: seed logged as $SEED"

echo ""
echo "=== All property integration checks passed ==="
