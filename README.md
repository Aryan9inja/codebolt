# CodeBolt

![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go)
![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg?style=for-the-badge)
![Status](https://img.shields.io/badge/Status-Beta-brightgreen.svg?style=for-the-badge)

**A self-hostable GitHub PR review agent that combines deterministic AST analysis with LLM reasoning - so enterprises get auditable, low-noise findings without sending their code to a third-party API.**

Pure Go. No frontend — GitHub itself is the UI. Pull requests in, inline review comments out.

---

## Why CodeBolt

CodeBolt is positioned against tools like CodeRabbit, Qodo, Sourcegraph Cody, and GitHub Copilot's PR review. The differentiators:

- **Self-hosted** — code never leaves the customer's infrastructure, built for data-residency-sensitive enterprises.
- **Deterministic + auditable** — every finding from the AST layer is reproducible and explainable, not a black-box LLM guess.
- **LLM as a genuine second reviewer, not a garnish** — every changed Go file gets a full LLM pass; nothing is skipped to save cost.
- **Cost discipline without compromise on coverage** — the cost lever is _bounded output tokens_ (structured, capped-length JSON), not gating which files get reviewed.

---

## Architecture & How It Works

CodeBolt triggers via GitHub webhooks and processes reviews asynchronously using [GoTaskQ](https://github.com/Aryan9inja/gotaskq).

```mermaid
graph TD
    A[GitHub Webhook] --> B(Webhook Handler)
    B --> C[GoTaskQ Async Job]
    C --> D{Fetch & Parse}
    D --> E[Fetch PR Diff & Files]
    E --> F[Diff Parser]
    
    subgraph Stream1 [Stream 1: AST Deterministic]
        G[AST Analysis]
    end
    
    subgraph Stream2 [Stream 2: LLM Selective Enhancement]
        H[LLM 3-Agent Pipeline] --> I(Detector)
        I --> J(Suggester)
        J --> K(Reviewer)
    end
    
    F --> G
    F --> H
    G -. "AST Findings (Context Only)" .-> I
    G --> L[Merge Findings]
    K --> L
    L --> M[Post GitHub PR Review]
```

### Two Independent Review Streams

**Stream 1 — AST Analysis (Deterministic)**
Go's `go/parser` walks each changed file against 17 rules through a dispatch table, producing structural and stylistic findings.

**Stream 2 — LLM Agent Pipeline (Selective Enhancement)**
A three-agent sequential pipeline per changed file, using AST findings as _context only_ (never re-emitted):

1. **Detector** — Finds new logic/behavioral issues AST can't catch. Given the full file + AST findings, it outputs a bounded candidate list.
2. **Suggester** — Given the Detector's candidates, attaches an explanation and a suggested fix to each.
3. **Reviewer** — The final quality gate. Assigns a confidence score (0.0–1.0) and a decision (`inline` / `summary` / `drop`).

The two streams are **never merged mid-pipeline**. They run independently and are concatenated only at the final API call to GitHub. 

### Why Three Agents?
A single-prompt design was explicitly considered and rejected. It would be cheaper, but it would have a higher chance of hallucinating because of context overload. The three-agent design is a deliberate tradeoff: more calls, but each call has a smaller context and a more focused task, reducing hallucination risk.

---

## Tech Stack

| Layer         | Choice                                                                 |
| ------------- | ---------------------------------------------------------------------- |
| **Language**  | Go 1.21+                                                               |
| **HTTP**      | `go-chi/chi`, `net/http`                                               |
| **Job Queue** | [GoTaskQ](https://github.com/Aryan9inja/gotaskq) v1.2                  |
| **AST Parsing**| `go/parser` (Phase 1) → tree-sitter (Phase 2, multi-language)         |
| **Vector DB** | Gemini Embeddings & pgvector on PostgreSQL (optional)                  |
| **LLM APIs**  | OpenRouter (free tier), Gemini API, provider-agnostic interface        |
| **Metrics**   | Prometheus                                                             |

---

## Quick Start & Self-Hosting

### 1. GitHub App Setup
1. Create a new GitHub App in your organization settings.
2. **Permissions**: 
   - Pull Requests: Read & Write
   - Contents: Read
3. **Webhook**: Set the URL to your CodeBolt instance (e.g. `https://your-domain.com/webhook`).
4. Generate and download a Private Key (`.pem` file).

### 2. Environment Variables
Create a `.env` file in the root directory:

```env
PORT=8080

# GitHub App details
GITHUB_WEBHOOK_SECRET=your_webhook_secret_here
GITHUB_APP_ID=123456
GITHUB_PRIVATE_KEY_PATH=./codebolt.pem

# LLM Providers (at least one required depending on configured pipeline)
OPENROUTER_API_KEY=your_openrouter_key
GEMINI_API_KEY=your_gemini_key

# Optional: Vector search for cross-PR context
DATABASE_URL=postgresql://postgres:postgres@localhost:5432/codebolt
```

### 3. Run the Server
```bash
go mod download
go build -o codebolt ./cmd/server
./codebolt
```

---

## Repository Structure

```
codebolt/
├── cmd/server/main.go          # Wires github client + llm provider/pipeline → processor
├── internal/
│   ├── analyzer/               # AST dispatch table + 17 deterministic rules
│   ├── diff/                   # Unified diff parser, DiffPosition tracking
│   ├── embeddings/             # Vector search via pgvector and Gemini
│   ├── github/                 # JWT, installation tokens, API wrapper
│   ├── llm/                    # 3-agent pipeline, prompts, provider implementations
│   ├── processor/              # Job handler (orchestrates AST + LLM streams)
│   └── webhook/                # HMAC validation, GitHub payload parsing
├── migrations/                 # Database migrations for pgvector setup
├── .env.example
└── go.mod
```

---

## Design Principles

- **AST and LLM are complementary, not hierarchical.** Neither is a pre-filter or post-filter for the other; they run independently.
- **Cost control = bounded output tokens, not selective invocation.** Every changed file gets an LLM pass — no allowlists, no severity gating.
- **Reject oversized diffs rather than silently truncate them.**
- **Isolate before assuming.** When a model/provider misbehaves, reproduce it via direct curl to the provider first.

---

## Current Status & Future Scope

CodeBolt is currently fully functional for **Go codebases**.

- [x] Webhook handling and HMAC signature validation
- [x] PR event parsing and GoTaskQ async job processing
- [x] GitHub App auth and Diff position tracking
- [x] AST analyzer with 17 rules
- [x] 3-agent LLM pipeline (Detector → Suggester → Reviewer)
- [x] Cross-PR pattern detection using pgvector and embeddings
- [x] Per-repo `codebolt.yaml` configuration

### Future Scope
- **Language expansion**: Python and JavaScript/TypeScript using `tree-sitter`.
- **Optimization pass**: Reducing the ~3 min/file LLM pipeline latency and managing `go.mod` fetches more efficiently.

---

## License

CodeBolt is licensed under the Apache 2.0 License.
