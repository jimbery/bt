# ADR-004: Native Go fuzzing for robustness mode

**Status:** Proposed  
**Date:** 2026-05-09

---

## Context

`bt` needs a mutation-heavy robustness testing mode for malformed, hostile, or boundary-pushing payloads. This complements property-based testing (which generates valid structured inputs) by exploring invalid, malformed, and unexpected inputs at a lower level.

Options considered:

- **Native Go fuzzing** (`go test -fuzz`, available from Go 1.18) — standard toolchain, no external dependency, corpus management built in
- **go-fuzz** — predates native support, now largely superseded, requires a separate build step
- **Custom mutation library** — full control, but significant implementation cost with no clear advantage over native toolchain support
- **Hypothesis/Atheris (Python)** — not applicable; Go-first decision already made in ADR-001

## Decision

Use Go's native fuzzing support (`go test -fuzz`) as the foundation for the fuzz strategy.

## Rationale

- **Standard toolchain** — no external dependency, no separate installation step, consistent with the single-binary distribution goal
- **Corpus management** is built in. Go's fuzzer maintains a corpus of interesting inputs across runs, which can be seeded from prior property-based failures or manually curated cases
- **Coverage-guided** — Go's fuzzer uses coverage instrumentation to guide mutation toward unexplored code paths, producing more interesting failures than naive random mutation
- **Complementary to property testing** — property testing (ADR-003) generates valid structured inputs and checks invariants; fuzzing generates malformed and boundary inputs and checks for crashes, panics, and unexpected behaviour. These are different tools for different failure classes

## Consequences

- Native fuzzing runs as a long-running process (`go test -fuzz=. -fuzztime=60s`). The fuzz strategy must manage this lifecycle and surface results through the standard `Result` and `Artifact` model (ADR-002)
- Fuzzing is inherently non-deterministic during exploration but deterministic on replay — a found input is a fixed byte sequence that can be replayed exactly. Replay bundles (M3) must store the raw fuzz corpus entry alongside the seed
- The safety model (M5) is critical for fuzz mode. Fuzzing must not run destructive operations without explicit opt-in, and method allow/deny lists must be enforced before any mutated request is sent
- Fuzz campaigns are better suited to longer-running nightly runs than to PR-gating. CI profiles should distinguish between a short fuzz smoke test and a full campaign