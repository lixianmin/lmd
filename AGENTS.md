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

## Key Technical Decisions

- Tokenizer: go-ego/gse (Chinese/English segmentation)
- Keyword search: gse pre-tokenize + FTS5 with unicode61 tokenizer
- Vector storage: sqlite-vec extension
- Default embedding model: Qwen3-Embedding-0.6B (GGUF)
- Fusion: RRF with MMR diversity re-ranking
- CLI framework: cobra
- Timezone: All timestamps in GMT+8 (CST)
- SQLite mode: WAL for concurrent reads

## Build & Test

```bash
go build ./cmd/lmd/          # Build CLI
go test ./... -v              # Run all tests
go vet ./...                  # Static analysis
```

## todo.md Processing Rules

The file `docs/todo.md` contains temporary development thoughts and ideas. When processing:
- For questions: answer and record the conclusion
- For design issues: update the spec and plan, then remove from todo.md
- For action items: complete the action, then remove from todo.md
- When all items are resolved, todo.md should become empty (except the header)
