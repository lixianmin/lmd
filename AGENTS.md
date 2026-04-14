# AGENTS.md

This file serves as the entry point for AI agents working on the LMD project.

## Project Overview

LMD (Local Markdown Docs) is a Go-based local hybrid search engine for Markdown documents with first-class Chinese language support. It serves as both a CLI tool and an importable Go library.

## Key Principles

1. **Standard library first**: Use Go standard library whenever possible. Only introduce external dependencies when the standard library cannot meet the requirement.

2. **Test-first (TDD)**: Always write unit tests before implementation code. This is a hard rule, not a suggestion.

3. **CLI-first**: All features must be accessible via CLI. The code library (`pkg/`) is a thin wrapper around the service layer.

## Project Structure

```
cmd/lmd/          - CLI entry point
internal/cli/     - Cobra command definitions
internal/service/ - Business logic layer
internal/store/   - SQLite persistence (FTS5 + sqlite-vec)
internal/tokenizer/ - Text segmentation (gse)
internal/embedding/  - Vector embedding abstraction
internal/chunker/    - Markdown-aware document chunking
internal/formatter/  - Output formatting (text/json/md/csv)
pkg/               - Public API for external Go projects
test/fixtures/     - Test markdown documents (Chinese + English)
```

## Design Documents

- Spec: `docs/superpowers/specs/2026-04-12-lmd-design.md`
- Phase 1 Plan: `docs/superpowers/plans/2026-04-12-lmd-phase1-foundation.md`
- Phase 2 Plan: `docs/superpowers/plans/2026-04-12-lmd-phase2-vector.md`
- Phase 3 Plan: `docs/superpowers/plans/2026-04-13-lmd-phase3-hybrid.md`
- Phase 4 Plan: `docs/superpowers/plans/2026-04-13-lmd-phase4-integration.md`

## Key Technical Decisions

- Tokenizer: go-ego/gse (Chinese/English segmentation) — use `seg.SkipLog = true` to suppress log noise
- Keyword search: gse pre-tokenize + FTS5 with unicode61 tokenizer
- Vector storage: sqlite-vec extension (asg017/sqlite-vec-go-bindings/cgo)
- Embedding model: Qwen3-Embedding-0.6B-Q8_0 (GGUF, 610MB)
- Embedding serving: llama-server HTTP API (port 61999, `--pooling mean --embedding`)
- **Query embedding: no Instruct prefix** — documents are embedded as-is, queries must also be embedded as-is (prefix mismatch causes bad results)
- Fusion: RRF (k=60) with optional MMR diversity re-ranking
- CLI framework: cobra
- Logging: github.com/lixianmin/logo
- Timezone: All timestamps in GMT+8 (CST)
- SQLite mode: WAL for concurrent reads
- DB operations: Prefer prepared statements for frequently used queries
- No migration system: single `CreateTables` function
- **Naming convention**: prefer `docId` over `docID` (camelCase with lowercase 'd' for second word)

## Build & Test

```bash
make build                    # Build CLI (tags: fts5)
make test                     # Run all tests (tags: fts5)
make vet                      # Static analysis
./lmd vsearch "query"         # Quick smoke test
```

## Reference Projects

- **QMD** (reference implementation): `/Users/xmli/me/code/others/qmd` — GitHub: https://github.com/tobi/qmd
  - TypeScript project with similar hybrid search architecture
  - Key reference for: embedding model integration, GGUF loading, HyDE, output formatting
  - Consult before making design decisions on embedding/LLM integration

## todo.md Processing Rules

The file `docs/todo.md` contains temporary development thoughts and ideas. When processing:
- For questions: answer and record the conclusion
- For design issues: update the spec and plan, then remove from todo.md
- For action items: complete the action, then remove from todo.md
- When all items are resolved, todo.md should become empty (except the header)
- Always check todo.md before starting a new task
