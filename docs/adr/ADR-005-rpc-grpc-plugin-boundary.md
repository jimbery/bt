# ADR-005: RPC/gRPC plugin boundary

**Status:** Proposed  
**Date:** 2026-05-09

---

## Context

`bt` will eventually need to support external plugins — custom protocol adapters, assertion packs, internal enterprise auth providers, and new strategy implementations. A plugin mechanism needs to be chosen that balances extensibility, safety, and long-term stability.

Options considered:

- **Native Go plugins** (`plugin` package) — shared objects loaded at runtime, same process, no serialisation overhead, but brittle: plugins must be compiled with the exact same Go version and dependencies as the host, and a panicking plugin crashes the host process
- **HashiCorp `go-plugin` over gRPC** — subprocess model, plugins run in a separate process, communicate over gRPC/RPC, host is isolated from plugin crashes, plugins can be written in any language that speaks gRPC
- **Embedded scripting** (Lua, Starlark, Tengo) — lightweight extension without a separate process, but limits what plugins can do and adds an interpreter dependency
- **HTTP sidecar** — plugins run as local HTTP servers, called by the host; flexible but adds significant operational complexity for plugin authors

## Decision

Use HashiCorp's `go-plugin` library with gRPC transport for the plugin boundary. Do not expose the plugin SDK publicly until the in-process strategy and adapter interfaces have stabilised through internal use.

## Rationale

- **Process isolation** — a misbehaving plugin cannot crash or corrupt the host process. This is critical for a tool running in CI where reliability is non-negotiable
- **Version stability** — gRPC protocol buffers provide a versioned, typed contract between host and plugin. Adding a field to a message is backwards-compatible; native Go plugins have no such guarantee
- **Language agnostic** — plugin authors can use any language that has a gRPC implementation. This matters for enterprise adapters where the plugin author may not be a Go developer
- **Proven model** — Terraform, Packer, and Vault all use this pattern at scale. The operational properties are well understood
- **Deferred until internal interfaces stabilise** — exposing a plugin API before the in-process interfaces are proven by real use locks in mistakes. The plugin contract should reflect what actually works, not what seemed reasonable upfront

## Consequences

- Plugin communication has serialisation overhead compared to in-process calls. Acceptable for the use cases (adapters, assertion packs) where plugin calls are coarse-grained, not hot-path per-request calls
- Plugin authors need a proto file and a small SDK shim. The SDK (`/pkg/plugin`) should be the primary documentation and onboarding surface
- The graph/state strategy's need for richer lifecycle semantics (ADR-002 consequence) should be resolved before the plugin interface is finalised, since strategy plugins will need to implement that lifecycle
- Enterprise use cases (RBAC, centralised artifact storage) are the most likely early plugin consumers and should inform the plugin contract design when that work begins