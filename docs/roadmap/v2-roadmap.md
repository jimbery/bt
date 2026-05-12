# bt — v2 Roadmap

## Overview

v2 builds depth on top of the v1 foundation. Where v1 established the platform — four testing strategies, replay, MCP interface, AI scaffolding, contract verification, and GraphQL support — v2 makes those capabilities smarter and more grounded in real-world usage.

The guiding principle for v2 is: **tests should reflect how your API is actually used, not just what its schema permits**. A schema tells you what inputs are valid. Real traffic tells you what inputs matter. v2 closes that gap.

v2 consists of four milestones. Each has a clear exit criterion before the next begins. The `.5` integration test milestones follow the pattern established in v1 — every new capability is exercised against the in-repo example APIs before the milestone is considered complete.

---

## What v2 is not

v2 deliberately defers reach in favour of depth:

- **No plugin SDK** — the codebase is still internal; the plugin boundary can wait until external adoption makes it necessary
- **No web UI** — the CLI and MCP interface are sufficient for v2; a web UI is a v3 concern
- **No gRPC adapter** — GraphQL property testing and the trace adapter are higher value; gRPC follows after
- **No drift detection / enterprise layer** — drift detection requires centralised storage and alerting integrations that belong in an enterprise tier; deferred to v3

---

## Milestones

### M10 — Table Strategy Response Schema Assertions
**Weeks 1–2**

A targeted fix to a gap identified at the end of v1: the table strategy asserts status codes and headers but cannot validate response body schemas. Every other strategy (property, contract, GraphQL) validates response bodies field-by-field. The table strategy should too.

This is a small milestone but an important one — it corrects the foundation before v2 builds on top of it.

**Deliverables:**
- `expected.schema` field added to `CaseExpectation` — references a schema inline or by `$ref` to the OpenAPI/GraphQL schema
- `expected.schema` validation wired into the table strategy's `Execute` method using the existing `EvaluateBody` logic from the contract strategy
- Table test YAML format extended to support inline schema assertions
- `CaseExpectation.GQLDataSchema` — GraphQL-specific variant that validates `data.*` rather than the top-level body
- Existing table test cases in M2.5–M9.5 updated to include schema assertions where appropriate
- `bt_scaffold_config` MCP tool updated to include `expected.schema` in generated table cases when a response schema is available

**Exit criterion:** A table test case can declare `expected.schema` inline and the runner validates the response body against it, reporting field-level violations with full paths. Existing table tests that only assert status codes continue to pass unchanged. The orders API and GraphQL API integration test suites are updated to include at least one schema-asserting case per operation.

---

### M10.5 — Integration Test Suite (Table Schema Assertions)

Exercises M10's schema assertion capability against both in-repo APIs.

**Deliverables:**
- Orders API table cases updated with `expected.schema` for all passing operations — `GetHealth`, `ListOrders`, `CreateOrder`, `GetOrder`, `PatchOrder`
- GraphQL API table cases updated with `gql_data_schema` for `health`, `orders`, `createOrder`
- A deliberately wrong schema assertion added and confirmed to fail with the correct field path in the report
- CI confirms both APIs pass with schema assertions enabled

**Exit criterion:** All schema-asserting table cases pass in CI. A misconfigured schema assertion fails with a violation report that names the specific field, expected type, and actual value — not just "schema mismatch".

---

### M11 — GraphQL Property Testing
**Weeks 3–10**

Generative testing for GraphQL, deferred from M9. Where M9 delivered table and contract testing for GraphQL, M11 adds the property strategy — generating valid variable combinations from SDL types, running them against the API, and shrinking failures to minimal reproducible cases.

The core challenge is the variable generator: SDL types are richer than the OpenAPI type system in some ways (non-null, union types, interfaces) and the generator needs to handle all of them correctly without producing inputs that are trivially invalid.

**Deliverables:**
- SDL-derived variable generator (`internal/strategy/graphql/gen`) — maps SDL argument types to Rapid generators using the same approach as the OpenAPI generator in M4
- Type coverage:
  - Scalars: `String`, `Int`, `Float`, `Boolean`, `ID`
  - Enums: generates only declared values
  - Input objects: recursively generates all fields, respecting required vs optional
  - Lists: generates arrays of the item type, including empty lists
  - Non-null (`T!`): never generates null; nullable (`T`): generates null with low probability
  - Custom scalars: treated as `String` with a warning logged
- `response_matches_schema` invariant for GraphQL — validates `data.*` against the SDL-derived selection schema, using the existing `AssertResponse` logic from M9
- `no_gql_errors` invariant — fails if `errors` is present and non-null in the response (configurable: treat as warning or critical)
- Seed and replay metadata in artifact bundles — same mechanism as the REST property strategy
- `--seed` flag works identically for GraphQL property runs
- `bt_suggest_invariants` MCP tool updated to suggest GraphQL-appropriate invariants when the operation kind is `Query` or `Mutation`

**Exit criterion:** `bt run --strategy property --adapter graphql` generates variable combinations from SDL argument types, finds the `amount` type violation in the GraphQL API's broken resolver within 50 checks, shrinks the failure to a minimal input, and writes an artifact bundle. The failure is reproducible with `--seed`. All unit tests pass with `-race`.

---

### M11.5 — Integration Test Suite (GraphQL Property Testing)

Exercises M11's GraphQL property testing against the in-repo GraphQL API.

**Deliverables:**
- Property test config for the GraphQL API (`examples/graphql-api/bt/backendtest-property.yaml`)
- `no_gql_errors` and `response_matches_schema` invariants configured for `createOrder` and `order` operations
- CI confirms the `amount` type violation in the broken resolver is found automatically within 50 checks
- Artifact bundle produced for the violation — confirmed replayable with `--seed`
- `bt_suggest_invariants` called against the GraphQL schema in CI stub-path test — confirms GraphQL-appropriate suggestions returned

**Exit criterion:** `bt run --strategy property --adapter graphql` finds the broken resolver's schema violation automatically in CI. The failure artifact replays correctly. All prior CI steps continue to pass.

---

### M12 — Trace Adapter (HAR Import)
**Weeks 11–22**

The trace adapter learns from real traffic captured in HAR files and uses that knowledge to generate synthetic test cases that reflect how your API is actually used — not just what its schema permits.

This is the most architecturally novel milestone in v2. It does not replay captured requests verbatim (which would be brittle and produce no new coverage). Instead it is a two-stage pipeline:

**Stage 1 — Analysis:** parse the HAR file and extract statistical patterns from the captured traffic:
- Which operations are called, and how often (frequency distribution)
- What value distributions look like for each argument (e.g. `amount` ranges from 10–500, `currency` is GBP 70% / USD 25% / EUR 5%)
- What sequences of operations co-occur within a session (e.g. `POST /orders` is almost always followed by `GET /orders/{id}`)
- Which fields appear in requests even though the schema marks them optional

**Stage 2 — Synthesis:** use those patterns to produce a `TraceProfile` — a structured description of realistic usage that the property and fuzz strategies consume instead of their default uniform generators. The `TraceProfile` is written to `.bt/trace/profile.json` and referenced in `backendtest.yaml`.

This approach means:
- No real user data is stored or replayed — only statistical patterns extracted from it
- The property strategy generates inputs weighted towards realistic values rather than random schema-valid noise
- The fuzz strategy starts from realistic baseline inputs rather than schema-generated ones
- Sequence information seeds the stateful strategy in M13

**Deliverables:**
- HAR parser (`internal/trace/har`) — parses HAR 1.2 format, extracts entries matching a configurable service filter
- Pattern extractor (`internal/trace/analyze`) — produces frequency, value distribution, and sequence data from parsed entries
- `TraceProfile` model (`pkg/model/trace.go`) — structured representation of extracted patterns; JSON-serialisable
- `bt trace import <har-file>` command — runs the analysis pipeline and writes `TraceProfile` to `.bt/trace/profile.json`
- `bt trace inspect` command — prints a human-readable summary of the loaded profile (operation frequencies, top value distributions, inferred sequences)
- Property strategy integration — when a `TraceProfile` is present, generators use the extracted value distributions rather than uniform random; falls back to uniform when no profile is present
- Fuzz strategy integration — corpus seeded from profile-derived realistic inputs rather than schema-generated ones
- `backendtest.yaml` extended with optional `trace.profile` key pointing to a profile file
- `bt_suggest_strategy` MCP tool updated to recommend trace-informed property testing when a profile is present

**Explicitly out of scope for M12:**
- Live traffic capture (proxy, eBPF, agent) — HAR file import only
- PII detection or redaction — the profile contains only statistical patterns, not raw values; raw values never leave the HAR parser
- Datadog or other APM integrations — the Datadog spans API can inform frequency data in a future milestone; HAR is the only source for M12
- Sequence-based test generation — sequences are extracted and stored in the profile but consumed by the stateful strategy in M13

**Exit criterion:** `bt trace import <har-file>` parses a HAR file, extracts operation frequency and value distribution patterns, and writes a `TraceProfile` to `.bt/trace/profile.json`. `bt run --strategy property` with a profile configured generates inputs weighted towards the captured distributions — confirmed by checking that generated `currency` values match the HAR-observed distribution within a reasonable tolerance over 1000 draws. All unit tests pass with `-race`.

---

### M12.5 — Integration Test Suite (Trace Adapter)

Exercises M12's trace adapter against the in-repo orders API.

**Deliverables:**
- A representative HAR file (`examples/orders-api/bt/trace/sample.har`) — generated by running a scripted session against the orders API and capturing with a proxy or test client; committed to the repo as a fixture
- HAR fixture contents: realistic distribution of operations (70% GET, 20% POST, 10% PATCH), realistic `amount` values (10–200), `currency` weighted GBP/USD/EUR, `status` filter weighted towards `pending`
- `bt trace import` run against the fixture in CI — confirms profile is written correctly
- `bt trace inspect` output verified in CI — confirms frequency and distribution data is present and non-empty
- Property run with profile configured — confirmed to use weighted generators (checked via distribution test over 500 draws)
- `bt trace inspect` output schema verified — every field in the JSON output has a non-empty value

**Exit criterion:** `bt trace import examples/orders-api/bt/trace/sample.har` produces a valid profile in CI. A property run with the profile configured generates `currency` values matching the HAR distribution (GBP >60% of draws) over 500 draws. All prior CI steps continue to pass.

---

### M13 — Stateful Strategy
**Weeks 23–34**

The stateful strategy executes multi-step test flows where the output of one operation feeds the input of the next. This is the gap between "test individual operations in isolation" and "test real user journeys".

The stateful strategy is powered by two sources:
1. **Sequence data from the `TraceProfile`** (M12) — inferred from HAR sessions, describes which operations tend to follow which
2. **Hand-authored flow definitions** — YAML files that explicitly declare a sequence of steps with data bindings between them

Both sources produce the same internal representation — a `Flow` — which the stateful runner executes. Flows from the trace profile are generated automatically; hand-authored flows give teams precise control over specific journeys they want to test.

**Deliverables:**
- `Flow` model (`pkg/model/flow.go`):
  ```
  Flow: a named sequence of Steps
  Step: an operation ID, variable bindings, and assertions
  Binding: extracts a value from the previous step's response and injects it into the next step's input
  ```
- Flow generator (`internal/strategy/stateful/gen`) — produces `[]Flow` from a `TraceProfile` by combining the sequence data with the operation schemas
- Flow YAML loader — loads hand-authored flow definitions from `flows/` directory
- Stateful runner (`internal/strategy/stateful`) — executes a `Flow`, propagating bindings between steps, collecting a `FlowResult` per flow
- `FlowResult` model — extends `Result` with per-step detail: which step failed, what the binding values were, what the response was at each step
- Assertion model for flows: each step can declare its own `expected.status_code` and `expected.schema`; bindings that fail to extract (e.g. `$.id` on a null response) are `Critical` failures
- Artifact model extended to capture full flow execution: all steps, all bindings, all responses — sufficient to replay the entire flow with one command
- `bt replay` updated to replay flow artifacts step-by-step
- `bt_run` MCP tool updated to accept `strategy: stateful` and return per-flow results
- `bt_suggest_strategy` MCP tool updated to recommend stateful testing when a trace profile with sequence data is present

**Hand-authored flow YAML format:**
```yaml
flows:
  - id: create-and-retrieve-order
    description: "Create an order then retrieve it by the returned ID"
    steps:
      - id: create
        operation_id: CreateOrder
        input:
          body:
            amount: 100
            currency: GBP
        expected:
          status_code: 201
          schema:
            type: object
            required: [id, status]
            properties:
              id:     { type: string }
              status: { type: string, enum: [pending] }
        extract:
          order_id: "$.id"

      - id: retrieve
        operation_id: GetOrder
        input:
          path: "/orders/{order_id}"
        expected:
          status_code: 200
          schema:
            type: object
            required: [id, amount, status]
            properties:
              id:     { type: string }
              amount: { type: integer }
              status: { type: string }
```

**Exit criterion:** `bt run --strategy stateful` executes a hand-authored `create-and-retrieve-order` flow against the orders API, propagates the `id` binding correctly, and reports pass/fail per step with the binding values visible in the report. A flow generated automatically from the trace profile (M12) runs without error. A step failure produces an artifact that replays all steps up to and including the failure. All unit tests pass with `-race`.

---

### M13.5 — Integration Test Suite (Stateful Strategy)

Exercises M13's stateful strategy against both in-repo APIs.

**Deliverables:**
- Hand-authored flows for the orders API (`examples/orders-api/bt/flows/`):
  - `create-and-retrieve.yaml` — create an order, retrieve it by ID, assert status is `pending`
  - `create-and-update.yaml` — create an order, update its status to `confirmed`, assert the update is reflected
  - `create-and-cancel.yaml` — create an order, cancel it, assert `cancelled_at` is present and a string
- Hand-authored flow for the GraphQL API (`examples/graphql-api/bt/flows/`):
  - `create-and-query.yaml` — `createOrder` mutation, then `order` query binding on the returned ID
- Trace-generated flows run against both APIs — confirmed at least one valid flow is generated from the HAR fixture profile
- Per-step failure reporting verified — a deliberately broken flow (wrong expected status on step 2) produces a `FlowResult` that correctly identifies step 2 as the failing step
- Flow artifact replay verified — `bt replay` re-executes all steps of a failed flow
- CI runs all hand-authored flows and the trace-generated flows; all pass

**Exit criterion:** All hand-authored flows pass in CI against both APIs. A trace-generated flow runs without error. A deliberately failing flow reports the correct failing step with binding values and response detail in the artifact. All prior CI steps continue to pass. The complete CI pipeline runs ten integration layers end-to-end.

---

## v2 exit criterion

A team can point `bt` at a HAR file captured from real traffic, import a trace profile, and immediately run property tests and stateful flows that reflect how their API is actually used — not just what its schema allows. GraphQL APIs are tested generatively with the same depth as REST APIs. Table tests validate response schemas field-by-field, not just status codes. The platform finds the kind of bugs that only emerge from realistic usage patterns, not synthetic generation.

---

## Deferred to v3

| Item | Rationale |
|------|-----------|
| Drift detection | Requires centralised storage and alerting — enterprise tier |
| Plugin SDK | No external adoption yet; internal codebase is sufficient |
| Web UI | CLI and MCP interface cover v2 needs |
| gRPC adapter | GraphQL property testing and trace adapter are higher priority |
| Live traffic capture | HAR import covers v2; proxy/eBPF capture is v3 |
| Datadog APM integration | Spans API lacks request/response bodies; HAR is the better source for now |
| PII detection / redaction | Trace profile contains only statistical patterns; raw data never stored |

---

## ADRs required before M12 implementation begins

The trace adapter introduces new architectural decisions that need to be written and reviewed before coding starts:

| ADR | Decision needed |
|-----|----------------|
| ADR-008 | TraceProfile storage format and location — JSON in `.bt/trace/` vs embedded in `backendtest.yaml` |
| ADR-009 | Generator composition — how trace-derived distributions compose with schema-derived generators in the property strategy |
| ADR-010 | Sequence representation — how operation sequences are modelled in `TraceProfile` and consumed by the stateful strategy |
| ADR-011 | Flow binding model — how values are extracted from responses and injected into subsequent requests; JSONPath vs custom expression language |

ADRs for M10 and M11 are not required — both milestones extend existing patterns without introducing new architectural decisions.
