# CodeBolt

**A self-hostable GitHub PR review agent that combines deterministic AST analysis with LLM reasoning - so enterprises get auditable, low-noise findings without sending their code to a third-party API.**

Pure Go. No frontend — GitHub itself is the UI. Pull requests in, inline review comments out.

```
webhook → GoTaskQ → diff fetch → diff parse → AST analysis → LLM agent pipeline → merge → inline PR comments
```

> Author: [Aryan9inja](https://github.com/Aryan9inja) · Main repo: `github.com/Aryan9inja/codebolt`

---

## Why CodeBolt

CodeBolt is positioned against tools like CodeRabbit, Qodo, Sourcegraph Cody, and GitHub Copilot's PR review. The differentiators:

- **Self-hosted** — code never leaves the customer's infrastructure, built for data-residency-sensitive enterprises.
- **Deterministic + auditable** — every finding from the AST layer is reproducible and explainable, not a black-box LLM guess.
- **LLM as a genuine second reviewer, not a garnish** — every changed Go file gets a full LLM pass; nothing is skipped to save cost.
- **Cost discipline without compromise on coverage** — the cost lever is _bounded output tokens_ (structured, capped-length JSON), not gating which files get reviewed.

---

## How It Works

### Two independent review streams, merged once at the end

**Stream 1 — AST Analysis (deterministic)**
Go's `go/parser` walks each changed file against 17 rules through a dispatch table, producing structural/style findings.

**Stream 2 — LLM Agent Pipeline (selective enhancement)**
A three-agent sequential pipeline per changed file, using AST findings as _context only_ (never re-emitted):

1. **Detector** — given the full file + AST findings (framed as "already covered, don't re-flag"), finds only new logic/behavioral issues AST can't catch by design. Outputs a bounded candidate list.
2. **Suggester** — given only the Detector's candidates, attaches an explanation and a suggested fix to each.
3. **Reviewer** — given only the Suggester's output, acts as the final quality gate: assigns a confidence score (0.0–1.0) and a decision (`inline` / `summary` / `drop`).

The two streams are **never merged mid-pipeline**. AST findings become `ReviewComment`s exactly as before; LLM findings (`EnhancedFinding`) go through the same inline/summary split based on diff position, and both lists are concatenated only at the final `PostReview` call in `processor.go`. This mirrors the product pitch: AST is deterministic and auditable, LLM is independent and additive.

### Why three agents instead of one call

A single-prompt design was explicitly considered and rejected. It would be cheaper, but it would have higher chance to hallucinate because of context overload. The three-agent design is a deliberate tradeoff: more calls, but each call has a smaller context and a more focused task, which reduces hallucination risk.

### Async by design

The full 3-call pipeline currently takes ~3 minutes per file on free-tier model latency. This is acceptable because review posting happens asynchronously via GoTaskQ — it never blocks the webhook response. Also the high latency is because of the free-tier model; a paid-tier model would be faster. The async design is future-proofed for both latency and cost.

---

## Tech Stack

| Layer         | Choice                                                                 |
| ------------- | ---------------------------------------------------------------------- |
| Language      | Go 1.26.4                                                              |
| HTTP routing  | chi, `net/http`                                                        |
| Job queue     | [GoTaskQ](https://github.com/Aryan9inja/gotaskq) v1.2 (My own library) |
| AST parsing   | `go/parser` (Phase 1) → tree-sitter (Phase 2, multi-language)          |
| Vector search | Gemini Embeddings (implemented), pgvector on PostgreSQL (planned)      |
| LLM provider  | OpenRouter (free tier), Gemini API, provider-agnostic interface        |
| Metrics       | Prometheus                                                             |
| Deployment    | Fly.io(decided, may change)                                            |

---

## Repository Structure

```
codebolt/
├── cmd/server/main.go          # wires github client + llm provider/pipeline → processor
├── internal/
│   ├── analyzer/                # AST dispatch table + 17 rules
│   ├── diff/                   # unified diff parser, DiffPosition tracking
│   ├── github/                 # JWT, installation tokens, diff fetch, PostReview
│   └── llm/                     # provider interface, OpenRouter impl, prompts, 3-agent pipeline
│   ├── processor/               # job handler — orchestrates AST + LLM streams, merges, posts review
│   ├── webhook/                # HMAC validation, payload parsing
├── .env
└── go.mod
```

---

## Current Status

- [x] Webhook handling with HMAC signature validation
- [x] PR event parsing, GoTaskQ-backed async job processing
- [x] GitHub App auth (JWT → installation token), diff fetch, file content fetch
- [x] Unified diff parser with accurate `DiffPosition` tracking (distinct from file line numbers)
- [x] AST analyzer — 17 rules across a dispatch table, import-cycle-free via a leaf `analyzer/types` package
- [x] GitHub Reviews API integration — inline comments for diff-visible lines, file-level summary notes otherwise
- [x] Full 3-agent LLM pipeline (Detector → Suggester → Reviewer), live-tested end-to-end via ngrok against a real PR — correctly caught an injected off-by-one bug at 0.95 confidence with a working suggested fix
- [x] `go.mod` version detection feeding real Go-version context into AST rules (previously hardcoded empty)
- [x] Clean separation of AST and LLM result streams, merged only at comment-building time
- [x] LLM Provider Selector and Gemini Support: Implemented multiple model support (Gemini) with retry logic and increased token limits
- [x] Optional embeddings support built around Gemini integration

**Locked model:** Initially `poolside/laguna-m.1:free` via OpenRouter, selected after a larger reasoning model (Nemotron-3-Ultra-550B) proved too slow/throttled on the free tier. Gemini support has since been added as a secondary and potentially primary provider, complete with custom retry logic and updated token limits. Debugging approach: isolate provider issues via direct curl before assuming an application bug — this resolved the Nemotron timeout confusion cleanly and is the pattern to reuse for future model swaps.

---

## Future Scope

### Near-term

- **pgvector on PostgreSQL** — cross-PR pattern detection, e.g. surfacing "this exact pattern was flagged/fixed in PR #N before." Foundational embeddings package is implemented using Gemini; database layer not yet wired in.
- **Per-repo `codebolt.yaml` config loader** — the dispatch-map architecture has been ready since Day 6; loading hasn't been built. Will extend to LLM pipeline config too (e.g. per-repo model override) since the provider selector exists now.
- **`error-ignored` rule** — currently a stub; needs `go/types` for proper implementation.

### Language expansion — Python and JavaScript/TypeScript

CodeBolt is currently Go-only end to end, including the LLM system prompts, which explicitly say "Go code review pipeline." Planned expansion:

- Swap `go/parser` for **tree-sitter** (already the planned Phase 2 AST approach), enabling a shared multi-language parsing layer instead of a Go-specific one.
- Generalize the analyzer rule dispatch to be language-aware, with rule sets defined per language rather than hardcoded for Go.
- Generalize LLM prompts to be language-parameterized instead of Go-specific.
- Likely sequencing: Python first (simpler AST surface, large open-source PR corpus for testing), then JS/TS.

### Optimization pass (deferred)

Explicitly deferred until functional breadth is complete: LLM pipeline latency (~3 min/file), the additional `go.mod` fetch per PR, and general cost tuning. Functional features ship first; optimization happens as a dedicated pass afterward — not proactively mid-build.

---

## Design Principles

- **AST and LLM are complementary, not hierarchical.** Neither is a pre-filter or post-filter for the other; they run independently and merge only at output.
- **Cost control = bounded output tokens, not selective invocation.** Every changed file gets an LLM pass — no allowlists, no severity gating, no budget caps on which files are reviewed.
- **Reject oversized diffs rather than silently truncate them.**
- **Isolate before assuming the bug is in CodeBolt.** When a model/provider misbehaves, reproduce it via direct curl to the provider first.

---

## License

_(To be finalized — see GoTaskQ for licensing precedent: Apache 2.0.)_
