# M1 — Foundation

This document follows a strict order: spec first, tests second, implementation third. No implementation file should be written until the tests for it exist. The tests are the spec — if a test is unclear or awkward to write, the design needs revisiting before any code is written.

---

## Overview

M1 delivers the structural skeleton of `bt`. Nothing generative, no real HTTP calls — just a working CLI that can read config, validate it, and scaffold a starter file. Every domain type, interface, and CLI command introduced here will be inherited by every later milestone, so correctness and clarity matter more than speed.

**Exit criterion:** `bt init` scaffolds a valid `backendtest.yaml` in the current directory. `bt validate` checks it against schema and reports errors clearly. Both commands exit 0 on success and 2 on config or execution error.

---

## Step 1 — Initialise the repository

No tests here — this is one-time setup.

```bash
mkdir bt && cd bt
git init
go mod init github.com/yourorg/bt   # replace with your actual module path
```

`.gitignore`:

```gitignore
# Build output
bt
dist/

# Runtime artifacts
.bt/

# GoReleaser
*.tar.gz
*.zip
checksums.txt

# Editor
.DS_Store
.idea/
.vscode/
```

Create the directory structure:

```bash
mkdir -p \
  cmd/bt \
  internal/cli \
  internal/config \
  internal/engine \
  internal/runner \
  internal/replay \
  internal/report \
  internal/safety \
  internal/logging \
  internal/adapter/openapi \
  internal/strategy/table \
  internal/strategy/property \
  internal/strategy/fuzz \
  internal/strategy/contract \
  internal/mcp/tools \
  pkg/model \
  docs/adr \
  examples/orders-api \
  testdata

find . -type d -empty -not -path './.git/*' -exec touch {}/.gitkeep \;
```

Install dependencies:

```bash
go get github.com/spf13/cobra@latest
go get github.com/spf13/viper@latest
go get go.uber.org/zap@latest
go mod tidy
```

---

## Step 2 — Domain model

The domain model is the foundation everything else builds on. Write tests that assert the types behave correctly as data — construction, zero values, and JSON round-trips — before writing the types themselves.

### Spec

- `Target` holds connection details for a single API under test: name, base URL, schema path, environment, and auth config
- `Operation` represents a single API operation normalised from any protocol adapter: method, path, parameters, request body schema, and response specs
- `SchemaRef` represents a JSON Schema node: type, format, properties, items, required fields, nullability, enums, and composition keywords (`oneOf`, `anyOf`)
- `Case` is a single executable test case: an operation ID, a concrete input (method, path, headers, query, body), an optional expectation, and free-form metadata
- `Result` is the outcome of executing a `Case`: pass/fail, status code, duration, detailed failures, and captured request/response payloads
- `Artifact` is a portable replay bundle produced on failure: strategy kind, seed, case ID, timestamp, environment, request/response payloads, failures, and an optional shrink trace
- `Invariant` names a check to apply to results, with optional config
- All types must be safe to marshal to and from JSON without loss

### Tests

`pkg/model/model_test.go`:

```go
package model_test

import (
    "encoding/json"
    "testing"
    "time"

    "github.com/yourorg/bt/pkg/model"
)

// Target

func TestTarget_ZeroValue(t *testing.T) {
    // A zero-value Target should be usable without panicking.
    var target model.Target
    if target.Name != "" {
        t.Errorf("expected empty Name, got %q", target.Name)
    }
    if target.Auth.Type != "" {
        t.Errorf("expected empty Auth.Type, got %q", target.Auth.Type)
    }
}

func TestTarget_JSONRoundTrip(t *testing.T) {
    original := model.Target{
        Name:        "orders-api",
        BaseURL:     "https://staging.example.com",
        SchemaPath:  "./openapi.yaml",
        Environment: "staging",
        Auth: model.AuthConfig{
            Type: "bearer",
            Env:  "ORDERS_API_TOKEN",
        },
    }

    data, err := json.Marshal(original)
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var decoded model.Target
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if decoded.Name != original.Name {
        t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
    }
    if decoded.BaseURL != original.BaseURL {
        t.Errorf("BaseURL: got %q, want %q", decoded.BaseURL, original.BaseURL)
    }
    if decoded.Auth.Type != original.Auth.Type {
        t.Errorf("Auth.Type: got %q, want %q", decoded.Auth.Type, original.Auth.Type)
    }
}

// Operation

func TestOperation_ZeroValue(t *testing.T) {
    var op model.Operation
    if op.Method != "" {
        t.Errorf("expected empty Method, got %q", op.Method)
    }
    if op.Parameters != nil {
        t.Errorf("expected nil Parameters slice, got %v", op.Parameters)
    }
    if op.RequestBody != nil {
        t.Errorf("expected nil RequestBody, got non-nil")
    }
}

func TestOperation_JSONRoundTrip(t *testing.T) {
    original := model.Operation{
        ID:     "CreateOrder",
        Method: "POST",
        Path:   "/orders",
        Tags:   []string{"orders"},
        Parameters: []model.Parameter{
            {
                Name:     "X-Idempotency-Key",
                In:       "header",
                Required: true,
                Schema:   &model.SchemaRef{Type: "string"},
            },
        },
        RequestBody: &model.SchemaRef{
            Type: "object",
            Properties: map[string]*model.SchemaRef{
                "amount": {Type: "integer"},
            },
            Required: []string{"amount"},
        },
        Responses: []model.ResponseSpec{
            {StatusCode: 201, Schema: &model.SchemaRef{Type: "object"}},
            {StatusCode: 400, Schema: &model.SchemaRef{Type: "object"}},
        },
    }

    data, err := json.Marshal(original)
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var decoded model.Operation
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if decoded.ID != original.ID {
        t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
    }
    if decoded.Method != original.Method {
        t.Errorf("Method: got %q, want %q", decoded.Method, original.Method)
    }
    if len(decoded.Parameters) != len(original.Parameters) {
        t.Errorf("Parameters length: got %d, want %d", len(decoded.Parameters), len(original.Parameters))
    }
    if decoded.RequestBody == nil {
        t.Error("expected non-nil RequestBody after round-trip")
    }
    if len(decoded.Responses) != 2 {
        t.Errorf("Responses length: got %d, want 2", len(decoded.Responses))
    }
}

// SchemaRef

func TestSchemaRef_NilSafe(t *testing.T) {
    // A nil SchemaRef pointer must marshal to "null" without error.
    var s *model.SchemaRef
    data, err := json.Marshal(s)
    if err != nil {
        t.Fatalf("marshal nil SchemaRef failed: %v", err)
    }
    if string(data) != "null" {
        t.Errorf("expected null, got %s", data)
    }
}

func TestSchemaRef_Composition(t *testing.T) {
    // oneOf and anyOf must survive a JSON round-trip without loss.
    original := model.SchemaRef{
        OneOf: []*model.SchemaRef{
            {Type: "string"},
            {Type: "integer"},
        },
        AnyOf: []*model.SchemaRef{
            {Type: "object", Nullable: true},
        },
    }

    data, err := json.Marshal(original)
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var decoded model.SchemaRef
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if len(decoded.OneOf) != 2 {
        t.Errorf("OneOf length: got %d, want 2", len(decoded.OneOf))
    }
    if len(decoded.AnyOf) != 1 {
        t.Errorf("AnyOf length: got %d, want 1", len(decoded.AnyOf))
    }
    if !decoded.AnyOf[0].Nullable {
        t.Error("expected AnyOf[0].Nullable to be true")
    }
}

// Case

func TestCase_ZeroValue(t *testing.T) {
    var c model.Case
    if c.Expected != nil {
        t.Error("expected nil Expected on zero Case")
    }
    if c.Meta != nil {
        t.Error("expected nil Meta on zero Case")
    }
}

func TestCase_JSONRoundTrip(t *testing.T) {
    original := model.Case{
        ID:          "case-001",
        OperationID: "CreateOrder",
        Input: model.CaseInput{
            Method:  "POST",
            Path:    "/orders",
            Headers: map[string]string{"Content-Type": "application/json"},
            Body:    map[string]any{"amount": 100},
        },
        Expected: &model.CaseExpectation{StatusCode: 201},
        Meta:     map[string]any{"source": "table"},
    }

    data, err := json.Marshal(original)
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var decoded model.Case
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if decoded.ID != original.ID {
        t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
    }
    if decoded.Expected == nil {
        t.Fatal("expected non-nil Expected after round-trip")
    }
    if decoded.Expected.StatusCode != 201 {
        t.Errorf("Expected.StatusCode: got %d, want 201", decoded.Expected.StatusCode)
    }
}

// Result

func TestResult_PassedWithNoFailures(t *testing.T) {
    r := model.Result{
        CaseID:     "case-001",
        Passed:     true,
        StatusCode: 200,
        Duration:   42 * time.Millisecond,
    }
    if !r.Passed {
        t.Error("expected Passed to be true")
    }
    if len(r.Failures) != 0 {
        t.Errorf("expected no failures, got %d", len(r.Failures))
    }
}

func TestResult_FailedWithFailures(t *testing.T) {
    r := model.Result{
        CaseID:     "case-002",
        Passed:     false,
        StatusCode: 500,
        Failures: []model.Failure{
            {
                Invariant: "no_5xx",
                Message:   "received status 500",
                Expected:  "status < 500",
                Actual:    500,
            },
        },
    }
    if r.Passed {
        t.Error("expected Passed to be false")
    }
    if len(r.Failures) != 1 {
        t.Errorf("expected 1 failure, got %d", len(r.Failures))
    }
    if r.Failures[0].Invariant != "no_5xx" {
        t.Errorf("Invariant: got %q, want %q", r.Failures[0].Invariant, "no_5xx")
    }
}

func TestResult_JSONRoundTrip(t *testing.T) {
    original := model.Result{
        CaseID:     "case-003",
        Passed:     false,
        StatusCode: 422,
        Duration:   10 * time.Millisecond,
        Failures:   []model.Failure{{Invariant: "response_matches_schema", Message: "field 'id' missing"}},
        Request:    model.RequestDetail{Method: "POST", URL: "https://staging.example.com/orders", Body: []byte(`{"amount":0}`)},
        Response:   model.ResponseDetail{StatusCode: 422, Body: []byte(`{"error":"invalid"}`)},
    }

    data, err := json.Marshal(original)
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var decoded model.Result
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if decoded.CaseID != original.CaseID {
        t.Errorf("CaseID: got %q, want %q", decoded.CaseID, original.CaseID)
    }
    if decoded.Passed != original.Passed {
        t.Errorf("Passed: got %v, want %v", decoded.Passed, original.Passed)
    }
    if string(decoded.Request.Body) != string(original.Request.Body) {
        t.Errorf("Request.Body: got %s, want %s", decoded.Request.Body, original.Request.Body)
    }
}

// Artifact

func TestArtifact_JSONRoundTrip(t *testing.T) {
    now := time.Now().UTC().Truncate(time.Second)
    original := model.Artifact{
        ID:           "artifact-001",
        StrategyKind: "property",
        Seed:         12345,
        CaseID:       "case-042",
        OccurredAt:   now,
        Environment:  "staging",
        Failures:     []model.Failure{{Invariant: "no_5xx", Message: "received 503"}},
        ShrinkTrace:  []string{"step-1: reduced body", "step-2: removed field"},
    }

    data, err := json.Marshal(original)
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var decoded model.Artifact
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if decoded.ID != original.ID {
        t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
    }
    if decoded.Seed != original.Seed {
        t.Errorf("Seed: got %d, want %d", decoded.Seed, original.Seed)
    }
    if !decoded.OccurredAt.Equal(original.OccurredAt) {
        t.Errorf("OccurredAt: got %v, want %v", decoded.OccurredAt, original.OccurredAt)
    }
    if len(decoded.ShrinkTrace) != 2 {
        t.Errorf("ShrinkTrace length: got %d, want 2", len(decoded.ShrinkTrace))
    }
}

// Invariant

func TestInvariant_JSONRoundTrip(t *testing.T) {
    original := model.Invariant{
        Name:   "no_5xx",
        Config: map[string]any{"exclude": []any{"503"}},
    }

    data, err := json.Marshal(original)
    if err != nil {
        t.Fatalf("marshal failed: %v", err)
    }

    var decoded model.Invariant
    if err := json.Unmarshal(data, &decoded); err != nil {
        t.Fatalf("unmarshal failed: %v", err)
    }

    if decoded.Name != original.Name {
        t.Errorf("Name: got %q, want %q", decoded.Name, original.Name)
    }
    if decoded.Config == nil {
        t.Error("expected non-nil Config after round-trip")
    }
}
```

### Implementation

Write the types to make the tests pass. All types live in `pkg/model/`.

`pkg/model/target.go`:

```go
package model

type Target struct {
    Name        string     `json:"name"`
    BaseURL     string     `json:"base_url"`
    SchemaPath  string     `json:"schema_path"`
    Environment string     `json:"environment"`
    Auth        AuthConfig `json:"auth"`
}

type AuthConfig struct {
    Type string `json:"type"`
    Env  string `json:"env"`
}
```

`pkg/model/operation.go`:

```go
package model

type Operation struct {
    ID          string         `json:"id"`
    Method      string         `json:"method"`
    Path        string         `json:"path"`
    Tags        []string       `json:"tags,omitempty"`
    Parameters  []Parameter    `json:"parameters,omitempty"`
    RequestBody *SchemaRef     `json:"request_body,omitempty"`
    Responses   []ResponseSpec `json:"responses,omitempty"`
}

type Parameter struct {
    Name     string     `json:"name"`
    In       string     `json:"in"`
    Required bool       `json:"required"`
    Schema   *SchemaRef `json:"schema,omitempty"`
}

type SchemaRef struct {
    Type       string                `json:"type,omitempty"`
    Format     string                `json:"format,omitempty"`
    Properties map[string]*SchemaRef `json:"properties,omitempty"`
    Items      *SchemaRef            `json:"items,omitempty"`
    Required   []string              `json:"required,omitempty"`
    Nullable   bool                  `json:"nullable,omitempty"`
    Enum       []any                 `json:"enum,omitempty"`
    OneOf      []*SchemaRef          `json:"oneOf,omitempty"`
    AnyOf      []*SchemaRef          `json:"anyOf,omitempty"`
}

type ResponseSpec struct {
    StatusCode int        `json:"status_code"`
    Schema     *SchemaRef `json:"schema,omitempty"`
}
```

`pkg/model/invariant.go`:

```go
package model

type Invariant struct {
    Name   string         `json:"name"`
    Config map[string]any `json:"config,omitempty"`
}
```

`pkg/model/case.go`:

```go
package model

type Case struct {
    ID          string           `json:"id"`
    OperationID string           `json:"operation_id"`
    Input       CaseInput        `json:"input"`
    Expected    *CaseExpectation `json:"expected,omitempty"`
    Meta        map[string]any   `json:"meta,omitempty"`
}

type CaseInput struct {
    Method  string            `json:"method"`
    Path    string            `json:"path"`
    Headers map[string]string `json:"headers,omitempty"`
    Query   map[string]string `json:"query,omitempty"`
    Body    any               `json:"body,omitempty"`
}

type CaseExpectation struct {
    StatusCode int               `json:"status_code,omitempty"`
    Schema     *SchemaRef        `json:"schema,omitempty"`
    Headers    map[string]string `json:"headers,omitempty"`
}
```

`pkg/model/result.go`:

```go
package model

import "time"

type Result struct {
    CaseID     string         `json:"case_id"`
    Passed     bool           `json:"passed"`
    StatusCode int            `json:"status_code"`
    Duration   time.Duration  `json:"duration_ms"`
    Failures   []Failure      `json:"failures,omitempty"`
    Request    RequestDetail  `json:"request"`
    Response   ResponseDetail `json:"response"`
}

type Failure struct {
    Invariant string `json:"invariant"`
    Message   string `json:"message"`
    Expected  any    `json:"expected,omitempty"`
    Actual    any    `json:"actual,omitempty"`
}

type RequestDetail struct {
    Method  string            `json:"method"`
    URL     string            `json:"url"`
    Headers map[string]string `json:"headers,omitempty"`
    Body    []byte            `json:"body,omitempty"`
}

type ResponseDetail struct {
    StatusCode int               `json:"status_code"`
    Headers    map[string]string `json:"headers,omitempty"`
    Body       []byte            `json:"body,omitempty"`
}
```

`pkg/model/artifact.go`:

```go
package model

import "time"

type Artifact struct {
    ID           string         `json:"id"`
    StrategyKind string         `json:"strategy_kind"`
    Seed         int64          `json:"seed,omitempty"`
    CaseID       string         `json:"case_id"`
    OccurredAt   time.Time      `json:"occurred_at"`
    Environment  string         `json:"environment"`
    Request      RequestDetail  `json:"request"`
    Response     ResponseDetail `json:"response"`
    Failures     []Failure      `json:"failures,omitempty"`
    ShrinkTrace  []string       `json:"shrink_trace,omitempty"`
}
```

Run the tests before moving on:

```bash
go test ./pkg/model/... -race -v
```

All tests must pass before proceeding to Step 3.

---

## Step 3 — Strategy interface

### Spec

- `Strategy` is the contract every testing strategy implements
- `Plan` receives a spec and a list of operations, and returns the cases to be executed — it must not make any network calls
- `Execute` receives cases and an `Executor`, runs them, and returns results — it must not know about reporting or artifact writing
- `Executor` is the minimal interface the engine exposes to strategies for making HTTP calls
- Strategy kinds are a closed set of constants: table, property, fuzz, contract, graph

### Tests

`internal/strategy/strategy_test.go`:

```go
package strategy_test

import (
    "context"
    "errors"
    "testing"

    "github.com/yourorg/bt/internal/strategy"
    "github.com/yourorg/bt/pkg/model"
)

// fakeStrategy is a test double that records calls made to it.
type fakeStrategy struct {
    name            strategy.Kind
    planCalled      bool
    execCalled      bool
    planErr         error
    execErr         error
    casesToReturn   []model.Case
    resultsToReturn []model.Result
}

func (f *fakeStrategy) Name() strategy.Kind { return f.name }

func (f *fakeStrategy) Plan(_ context.Context, _ strategy.Spec, _ []model.Operation) ([]model.Case, error) {
    f.planCalled = true
    return f.casesToReturn, f.planErr
}

func (f *fakeStrategy) Execute(_ context.Context, _ []model.Case, _ strategy.Executor) ([]model.Result, error) {
    f.execCalled = true
    return f.resultsToReturn, f.execErr
}

// fakeExecutor is a test double for the Executor interface.
type fakeExecutor struct {
    response model.ResponseDetail
    err      error
}

func (f *fakeExecutor) Run(_ context.Context, _ model.CaseInput) (model.ResponseDetail, error) {
    return f.response, f.err
}

func TestStrategy_PlanAndExecuteSequence(t *testing.T) {
    s := &fakeStrategy{
        name:            strategy.KindTable,
        casesToReturn:   []model.Case{{ID: "case-001", OperationID: "GetOrder"}},
        resultsToReturn: []model.Result{{CaseID: "case-001", Passed: true}},
    }
    exec := &fakeExecutor{response: model.ResponseDetail{StatusCode: 200}}

    ctx := context.Background()
    spec := strategy.Spec{Kind: strategy.KindTable}
    ops := []model.Operation{{ID: "GetOrder", Method: "GET", Path: "/orders/{id}"}}

    cases, err := s.Plan(ctx, spec, ops)
    if err != nil {
        t.Fatalf("Plan returned unexpected error: %v", err)
    }
    if !s.planCalled {
        t.Error("expected Plan to be called")
    }
    if len(cases) != 1 {
        t.Errorf("expected 1 case, got %d", len(cases))
    }

    results, err := s.Execute(ctx, cases, exec)
    if err != nil {
        t.Fatalf("Execute returned unexpected error: %v", err)
    }
    if !s.execCalled {
        t.Error("expected Execute to be called")
    }
    if len(results) != 1 {
        t.Errorf("expected 1 result, got %d", len(results))
    }
}

func TestStrategy_PlanError_DoesNotImplyExecuteCalled(t *testing.T) {
    // The interface does not enforce call order, but the engine must not
    // call Execute when Plan fails. This test documents the contract.
    s := &fakeStrategy{
        name:    strategy.KindProperty,
        planErr: errors.New("schema parse failed"),
    }

    _, err := s.Plan(context.Background(), strategy.Spec{Kind: strategy.KindProperty}, nil)
    if err == nil {
        t.Fatal("expected Plan to return an error")
    }
    if s.execCalled {
        t.Error("Execute should not have been called")
    }
}

func TestStrategy_KindConstants_NonEmptyAndDistinct(t *testing.T) {
    kinds := []strategy.Kind{
        strategy.KindTable,
        strategy.KindProperty,
        strategy.KindFuzz,
        strategy.KindContract,
        strategy.KindGraph,
    }

    seen := make(map[strategy.Kind]bool)
    for _, k := range kinds {
        if k == "" {
            t.Error("strategy kind must not be empty string")
        }
        if seen[k] {
            t.Errorf("duplicate strategy kind: %q", k)
        }
        seen[k] = true
    }
}

func TestExecutor_RunReturnsResponse(t *testing.T) {
    exec := &fakeExecutor{
        response: model.ResponseDetail{StatusCode: 201, Body: []byte(`{"id":"ord-1"}`)},
    }
    resp, err := exec.Run(context.Background(), model.CaseInput{Method: "POST", Path: "/orders"})
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if resp.StatusCode != 201 {
        t.Errorf("StatusCode: got %d, want 201", resp.StatusCode)
    }
}

func TestExecutor_RunPropagatesError(t *testing.T) {
    exec := &fakeExecutor{err: errors.New("connection refused")}
    _, err := exec.Run(context.Background(), model.CaseInput{Method: "GET", Path: "/orders"})
    if err == nil {
        t.Fatal("expected error from Run")
    }
}
```

### Implementation

`internal/strategy/strategy.go`:

```go
package strategy

import (
    "context"

    "github.com/yourorg/bt/pkg/model"
)

// Kind identifies which testing strategy to use.
type Kind string

const (
    KindTable    Kind = "table"
    KindProperty Kind = "property"
    KindFuzz     Kind = "fuzz"
    KindContract Kind = "contract"
    KindGraph    Kind = "graph"
)

// Spec carries the configuration for a single strategy run.
type Spec struct {
    Kind       Kind
    Operations []string
    Invariants []model.Invariant
    Config     map[string]any // each strategy casts this to its own typed config internally
}

// Strategy is the contract every testing strategy must implement.
// Plan must not make network calls.
// Execute must not know about reporting or artifact writing.
type Strategy interface {
    Name() Kind
    Plan(ctx context.Context, spec Spec, ops []model.Operation) ([]model.Case, error)
    Execute(ctx context.Context, cases []model.Case, exec Executor) ([]model.Result, error)
}

// Executor is the minimal interface the engine exposes to strategies.
type Executor interface {
    Run(ctx context.Context, input model.CaseInput) (model.ResponseDetail, error)
}
```

Run the tests:

```bash
go test ./internal/strategy/... -race -v
```

---

## Step 4 — Config loading

### Spec

- `Load` reads `backendtest.yaml` (or the path from `--config`) and returns a validated `Config` struct
- If the file does not exist, `Load` returns `ErrConfigNotFound`
- If the file is invalid YAML, `Load` returns a descriptive parse error
- If required fields are missing (`target.name`, `target.base_url`), `Load` returns a validation error naming the field
- `Scaffold` writes a valid starter `backendtest.yaml` to a given path
- `Scaffold` must not overwrite an existing file unless `force` is true
- The scaffolded file must itself pass `Load` without error
- Defaults are applied before validation: `report.formats` defaults to `["console"]`, `safety.profile` defaults to `"safe"`

### Tests

`internal/config/config_test.go`:

```go
package config_test

import (
    "errors"
    "os"
    "path/filepath"
    "testing"

    "github.com/yourorg/bt/internal/config"
)

func TestLoad_FileNotFound(t *testing.T) {
    _, err := config.Load("/nonexistent/path/backendtest.yaml")
    if !errors.Is(err, config.ErrConfigNotFound) {
        t.Errorf("expected ErrConfigNotFound, got %v", err)
    }
}

func TestLoad_InvalidYAML(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")
    if err := os.WriteFile(path, []byte(":::invalid yaml:::"), 0644); err != nil {
        t.Fatal(err)
    }
    _, err := config.Load(path)
    if err == nil {
        t.Fatal("expected error for invalid YAML, got nil")
    }
    if errors.Is(err, config.ErrConfigNotFound) {
        t.Error("should not return ErrConfigNotFound for invalid YAML")
    }
}

func TestLoad_MissingRequiredField_TargetName(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")
    yaml := "version: 1\ntarget:\n  base_url: https://staging.example.com\n"
    if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
        t.Fatal(err)
    }
    _, err := config.Load(path)
    if err == nil {
        t.Fatal("expected validation error for missing target.name")
    }
}

func TestLoad_MissingRequiredField_TargetBaseURL(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")
    yaml := "version: 1\ntarget:\n  name: orders-api\n"
    if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
        t.Fatal(err)
    }
    _, err := config.Load(path)
    if err == nil {
        t.Fatal("expected validation error for missing target.base_url")
    }
}

func TestLoad_ValidMinimalConfig(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")
    yaml := "version: 1\ntarget:\n  name: orders-api\n  base_url: https://staging.example.com\n  schema: ./openapi.yaml\n"
    if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
        t.Fatal(err)
    }
    cfg, err := config.Load(path)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if cfg.Target.Name != "orders-api" {
        t.Errorf("Target.Name: got %q, want %q", cfg.Target.Name, "orders-api")
    }
    if cfg.Target.BaseURL != "https://staging.example.com" {
        t.Errorf("Target.BaseURL: got %q, want %q", cfg.Target.BaseURL, "https://staging.example.com")
    }
}

func TestLoad_DefaultsApplied(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")
    yaml := "version: 1\ntarget:\n  name: orders-api\n  base_url: https://staging.example.com\n  schema: ./openapi.yaml\n"
    if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
        t.Fatal(err)
    }
    cfg, err := config.Load(path)
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if len(cfg.Report.Formats) == 0 {
        t.Error("expected default report formats to be applied")
    }
    if cfg.Report.Formats[0] != "console" {
        t.Errorf("default report format: got %q, want %q", cfg.Report.Formats[0], "console")
    }
    if cfg.Safety.Profile != "safe" {
        t.Errorf("default safety profile: got %q, want %q", cfg.Safety.Profile, "safe")
    }
}

func TestLoad_FullConfig(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")
    yaml := `version: 1
target:
  name: orders-api
  base_url: https://staging.example.com
  schema: ./openapi.yaml
  auth:
    type: bearer
    env: ORDERS_API_TOKEN
strategies:
  - type: table
    file: ./tests/orders-table.yaml
  - type: property
    operations: [CreateOrder, GetOrder]
    invariants:
      - no_5xx
      - response_matches_schema
    config:
      max_examples: 100
      seed: 12345
report:
  formats: [console, junit, json]
  output_dir: ./.bt/reports
safety:
  profile: non_destructive
  deny_methods: [DELETE]
`
    if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
        t.Fatal(err)
    }
    cfg, err := config.Load(path)
    if err != nil {
        t.Fatalf("unexpected error loading full config: %v", err)
    }
    if len(cfg.Strategies) != 2 {
        t.Errorf("Strategies length: got %d, want 2", len(cfg.Strategies))
    }
    if cfg.Strategies[1].Type != "property" {
        t.Errorf("Strategies[1].Type: got %q, want %q", cfg.Strategies[1].Type, "property")
    }
    if cfg.Safety.Profile != "non_destructive" {
        t.Errorf("Safety.Profile: got %q, want %q", cfg.Safety.Profile, "non_destructive")
    }
    if len(cfg.Safety.DenyMethods) != 1 || cfg.Safety.DenyMethods[0] != "DELETE" {
        t.Errorf("Safety.DenyMethods: got %v, want [DELETE]", cfg.Safety.DenyMethods)
    }
}

func TestScaffold_WritesValidFile(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")

    if err := config.Scaffold(path, false); err != nil {
        t.Fatalf("Scaffold returned unexpected error: %v", err)
    }

    data, err := os.ReadFile(path)
    if err != nil {
        t.Fatalf("could not read scaffolded file: %v", err)
    }
    if len(data) == 0 {
        t.Error("scaffolded file must not be empty")
    }

    // The scaffolded file must itself be loadable without error.
    if _, err := config.Load(path); err != nil {
        t.Errorf("scaffolded config failed to load: %v", err)
    }
}

func TestScaffold_RefusesOverwrite(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")
    if err := os.WriteFile(path, []byte("existing"), 0644); err != nil {
        t.Fatal(err)
    }
    if err := config.Scaffold(path, false); err == nil {
        t.Error("expected error when overwriting existing file without force flag")
    }
}

func TestScaffold_ForceOverwrites(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "backendtest.yaml")
    if err := os.WriteFile(path, []byte("existing"), 0644); err != nil {
        t.Fatal(err)
    }
    if err := config.Scaffold(path, true); err != nil {
        t.Errorf("unexpected error with force flag: %v", err)
    }
}
```

### Implementation

`internal/config/loader.go`:

```go
package config

import (
    "errors"
    "fmt"
    "os"

    "github.com/spf13/viper"
)

var ErrConfigNotFound = errors.New("config file not found")

type Config struct {
    Version    int
    Target     TargetConfig
    Strategies []StrategyConfig
    Report     ReportConfig
    Safety     SafetyConfig
}

type TargetConfig struct {
    Name       string
    BaseURL    string `mapstructure:"base_url"`
    SchemaPath string `mapstructure:"schema"`
    Auth       AuthConfig
}

type AuthConfig struct {
    Type string
    Env  string
}

type StrategyConfig struct {
    Type       string
    File       string
    Operations []string
    Invariants []string
    Config     map[string]any
}

type ReportConfig struct {
    Formats   []string
    OutputDir string `mapstructure:"output_dir"`
}

type SafetyConfig struct {
    Profile     string
    DenyMethods []string `mapstructure:"deny_methods"`
}

func Load(path string) (*Config, error) {
    if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
        return nil, ErrConfigNotFound
    }

    v := viper.New()
    v.SetConfigFile(path)

    if err := v.ReadInConfig(); err != nil {
        return nil, fmt.Errorf("parse error: %w", err)
    }

    applyDefaults(v)

    var cfg Config
    if err := v.Unmarshal(&cfg); err != nil {
        return nil, fmt.Errorf("unmarshal error: %w", err)
    }

    if err := validate(&cfg); err != nil {
        return nil, err
    }

    return &cfg, nil
}

func applyDefaults(v *viper.Viper) {
    v.SetDefault("report.formats", []string{"console"})
    v.SetDefault("report.output_dir", ".bt/reports")
    v.SetDefault("safety.profile", "safe")
}

func validate(cfg *Config) error {
    if cfg.Target.Name == "" {
        return errors.New("validation error: target.name is required")
    }
    if cfg.Target.BaseURL == "" {
        return errors.New("validation error: target.base_url is required")
    }
    return nil
}
```

`internal/config/scaffold.go`:

```go
package config

import (
    "errors"
    "fmt"
    "os"
)

const scaffoldTemplate = `version: 1

target:
  name: my-api
  base_url: https://staging.example.com
  schema: ./openapi.yaml
  auth:
    type: bearer
    env: API_TOKEN

strategies:
  - type: table
    file: ./tests/table.yaml

  - type: property
    operations: []
    invariants:
      - no_5xx
      - response_matches_schema
    config:
      max_examples: 100

report:
  formats: [console, json]
  output_dir: .bt/reports

safety:
  profile: safe
  deny_methods: [DELETE]
`

func Scaffold(path string, force bool) error {
    if !force {
        if _, err := os.Stat(path); err == nil {
            return errors.New("config file already exists; use --force to overwrite")
        }
    }
    if err := os.WriteFile(path, []byte(scaffoldTemplate), 0644); err != nil {
        return fmt.Errorf("could not write config: %w", err)
    }
    return nil
}
```

Run the tests:

```bash
go test ./internal/config/... -race -v
```

---

## Step 5 — CLI wiring

### Spec

- `bt init` calls `config.Scaffold` and prints a success message; exits 0 on success, non-zero on error
- `bt validate` calls `config.Load` and prints a success message or the validation error; exits 0 on success, non-zero on error
- Both commands respect `--config` for a custom config path, defaulting to `backendtest.yaml`
- Neither command makes any network calls
- Root command is constructed via `NewRootCmd()` so it can be tested without `os.Exit`

### Tests

`internal/cli/cli_test.go`:

```go
package cli_test

import (
    "bytes"
    "os"
    "path/filepath"
    "testing"

    "github.com/yourorg/bt/internal/cli"
)

func TestInitCommand_CreatesConfigFile(t *testing.T) {
    dir := t.TempDir()
    configPath := filepath.Join(dir, "backendtest.yaml")

    cmd := cli.NewRootCmd()
    cmd.SetArgs([]string{"init", "--config", configPath})
    cmd.SetOut(&bytes.Buffer{})

    if err := cmd.Execute(); err != nil {
        t.Fatalf("init command failed: %v", err)
    }
    if _, err := os.Stat(configPath); os.IsNotExist(err) {
        t.Error("expected config file to be created")
    }
}

func TestInitCommand_RefusesOverwriteWithoutForce(t *testing.T) {
    dir := t.TempDir()
    configPath := filepath.Join(dir, "backendtest.yaml")
    if err := os.WriteFile(configPath, []byte("existing"), 0644); err != nil {
        t.Fatal(err)
    }

    cmd := cli.NewRootCmd()
    cmd.SetArgs([]string{"init", "--config", configPath})
    if err := cmd.Execute(); err == nil {
        t.Error("expected error when config already exists without --force")
    }
}

func TestInitCommand_ForceOverwrites(t *testing.T) {
    dir := t.TempDir()
    configPath := filepath.Join(dir, "backendtest.yaml")
    if err := os.WriteFile(configPath, []byte("existing"), 0644); err != nil {
        t.Fatal(err)
    }

    cmd := cli.NewRootCmd()
    cmd.SetArgs([]string{"init", "--config", configPath, "--force"})
    cmd.SetOut(&bytes.Buffer{})
    if err := cmd.Execute(); err != nil {
        t.Errorf("expected init --force to succeed, got: %v", err)
    }
}

func TestValidateCommand_ValidConfig(t *testing.T) {
    dir := t.TempDir()
    configPath := filepath.Join(dir, "backendtest.yaml")
    yaml := "version: 1\ntarget:\n  name: orders-api\n  base_url: https://staging.example.com\n  schema: ./openapi.yaml\n"
    if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
        t.Fatal(err)
    }

    cmd := cli.NewRootCmd()
    cmd.SetArgs([]string{"validate", "--config", configPath})
    cmd.SetOut(&bytes.Buffer{})
    if err := cmd.Execute(); err != nil {
        t.Errorf("expected validate to succeed, got: %v", err)
    }
}

func TestValidateCommand_InvalidConfig(t *testing.T) {
    dir := t.TempDir()
    configPath := filepath.Join(dir, "backendtest.yaml")
    yaml := "version: 1\ntarget:\n  base_url: https://staging.example.com\n"
    if err := os.WriteFile(configPath, []byte(yaml), 0644); err != nil {
        t.Fatal(err)
    }

    cmd := cli.NewRootCmd()
    cmd.SetArgs([]string{"validate", "--config", configPath})
    if err := cmd.Execute(); err == nil {
        t.Error("expected validate to fail for invalid config")
    }
}

func TestValidateCommand_MissingFile(t *testing.T) {
    cmd := cli.NewRootCmd()
    cmd.SetArgs([]string{"validate", "--config", "/nonexistent/backendtest.yaml"})
    if err := cmd.Execute(); err == nil {
        t.Error("expected validate to fail for missing config file")
    }
}
```

### Implementation

`internal/cli/root.go`:

```go
package cli

import "github.com/spf13/cobra"

func NewRootCmd() *cobra.Command {
    root := &cobra.Command{
        Use:          "bt",
        Short:        "Backend testing platform",
        Long:         "bt is a Go-native backend testing platform for table, property, fuzz, and contract testing strategies.",
        SilenceUsage: true,
    }

    root.PersistentFlags().String("config", "backendtest.yaml", "config file path")
    root.PersistentFlags().String("env", "", "environment profile (local, ci, staging, preprod)")
    root.PersistentFlags().String("output", "console", "output format (console, json, junit)")

    root.AddCommand(newInitCmd())
    root.AddCommand(newValidateCmd())
    root.AddCommand(newRunCmd())
    root.AddCommand(newReplayCmd())
    root.AddCommand(newDoctorCmd())

    return root
}

func Execute() error {
    return NewRootCmd().Execute()
}
```

`internal/cli/init.go`:

```go
package cli

import (
    "fmt"

    "github.com/spf13/cobra"
    "github.com/yourorg/bt/internal/config"
)

func newInitCmd() *cobra.Command {
    var force bool

    cmd := &cobra.Command{
        Use:          "init",
        Short:        "Scaffold a backendtest.yaml in the current directory",
        SilenceUsage: true,
        RunE: func(cmd *cobra.Command, args []string) error {
            path, _ := cmd.Flags().GetString("config")
            if err := config.Scaffold(path, force); err != nil {
                return err
            }
            fmt.Fprintf(cmd.OutOrStdout(), "created %s\n", path)
            return nil
        },
    }

    cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config file")
    return cmd
}
```

`internal/cli/validate.go`:

```go
package cli

import (
    "fmt"

    "github.com/spf13/cobra"
    "github.com/yourorg/bt/internal/config"
)

func newValidateCmd() *cobra.Command {
    return &cobra.Command{
        Use:          "validate",
        Short:        "Validate the backendtest.yaml config file",
        SilenceUsage: true,
        RunE: func(cmd *cobra.Command, args []string) error {
            path, _ := cmd.Flags().GetString("config")
            if _, err := config.Load(path); err != nil {
                return err
            }
            fmt.Fprintf(cmd.OutOrStdout(), "config is valid\n")
            return nil
        },
    }
}
```

Stub remaining commands:

```go
// internal/cli/run.go
package cli
import "github.com/spf13/cobra"
func newRunCmd() *cobra.Command {
    return &cobra.Command{Use: "run", Short: "Run a test plan", SilenceUsage: true,
        RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}

// internal/cli/replay.go
package cli
import "github.com/spf13/cobra"
func newReplayCmd() *cobra.Command {
    return &cobra.Command{Use: "replay", Short: "Replay a test artifact", SilenceUsage: true,
        RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}

// internal/cli/doctor.go
package cli
import "github.com/spf13/cobra"
func newDoctorCmd() *cobra.Command {
    return &cobra.Command{Use: "doctor", Short: "Check environment and config", SilenceUsage: true,
        RunE: func(cmd *cobra.Command, args []string) error { return nil }}
}
```

`cmd/bt/main.go`:

```go
package main

import (
    "os"

    "github.com/yourorg/bt/internal/cli"
)

func main() {
    if err := cli.Execute(); err != nil {
        os.Exit(1)
    }
}
```

Run the tests:

```bash
go test ./internal/cli/... -race -v
```

---

## Step 6 — GitHub Actions and GoReleaser

No tests here — validated by running them in CI.

`.github/workflows/ci.yml`:

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - name: Lint
        uses: golangci/golangci-lint-action@v6
        with:
          version: latest
      - name: Test
        run: go test ./... -race -coverprofile=coverage.out
      - name: Build
        run: go build ./cmd/bt
```

`.github/workflows/release.yml`:

```yaml
name: Release

on:
  push:
    tags: ['v*']

jobs:
  release:
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - uses: goreleaser/goreleaser-action@v6
        with:
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

`.github/workflows/security.yml`:

```yaml
name: Security

on:
  schedule:
    - cron: '0 9 * * 1'
  workflow_dispatch:

jobs:
  scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - name: govulncheck
        uses: golang/govulncheck-action@v1
```

`.goreleaser.yaml`:

```yaml
version: 2

before:
  hooks:
    - go mod tidy

builds:
  - id: bt
    main: ./cmd/bt
    binary: bt
    env:
      - CGO_ENABLED=0
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]

archives:
  - name_template: "bt_{{ .Os }}_{{ .Arch }}"
    format: tar.gz
    format_overrides:
      - goos: windows
        format: zip

checksum:
  name_template: checksums.txt

changelog:
  sort: asc
  filters:
    exclude:
      - '^docs:'
      - '^test:'
      - '^chore:'
```

---

## Step 7 — Full verification

Run all tests together before marking M1 complete:

```bash
# All packages, race detector on
go test ./... -race -v

# Clean build
CGO_ENABLED=0 go build ./cmd/bt

# Smoke test the CLI
./bt --help
./bt init --config /tmp/bt-test/backendtest.yaml
./bt validate --config /tmp/bt-test/backendtest.yaml
```

Expected final state:
- All tests pass with `-race`
- Binary builds with `CGO_ENABLED=0`
- `bt init` creates a valid, loadable config file
- `bt validate` passes on the scaffolded file and returns a clear error on an invalid one
- CI passes on first push to main

---

## M1 exit criterion

`bt init` scaffolds a valid `backendtest.yaml`. `bt validate` checks it against schema and reports errors clearly. Both exit 0 on success and non-zero on failure. Every piece of code in this milestone has tests written before implementation.