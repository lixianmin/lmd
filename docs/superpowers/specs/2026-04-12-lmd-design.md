# LMD (Local Markdown Docs) - Design Specification

## Overview

LMD is a local hybrid search engine for Markdown documents written in Go. It builds personal knowledge bases with first-class Chinese language support, and serves as a memory layer for AI agents.

The project produces two deliverables:
1. A Go library (`pkg/`) importable by other Go projects
2. A CLI tool compiled from `cmd/lmd/`

## Background & Motivation

The design is inspired by [QMD](https://github.com/tobi/qmd) but addresses three key shortcomings:
- **Poor Chinese tokenization**: QMD uses SQLite FTS5's Unicode61 tokenizer which does not handle Chinese well
- **Slow local performance**: Default configuration causes slow operation on local machines
- **Suboptimal output format**: Search results need better formatting for personal knowledge management

## Key Decisions

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Language | Go | Performance, single binary, good SQLite ecosystem |
| Tokenizer | go-ego/gse | Go-native jieba implementation, excellent Chinese support |
| Vector storage | sqlite-vec | C-optimized SIMD, low Go memory footprint |
| Keyword search | gse pre-tokenize + FTS5 simple | Avoids Unicode61, leverages mature FTS5 BM25 |
| Embedding model | Qwen3-Embedding-0.6B (GGUF) | 119 languages including CJK, MTEB top-ranked |
| Fusion strategy | RRF priority + optional reranking | Fast by default, quality when needed |
| Query expansion | Optional | User-controlled complexity |
| CLI framework | cobra | Subcommand support, auto-help, shell completion |
| Agent integration | MCP Server + CLI JSON output | Dual access for maximum compatibility |
| Document chunking | Markdown-aware | Respects heading/code block boundaries |
| Collection storage | Single SQLite DB | Convenient cross-collection search |
| Architecture | Layered pipeline (CLI → Service → Store) | Clear separation, testable, extensible |

## Architecture

### Directory Structure

```
lmd/
├── cmd/
│   └── lmd/
│       └── main.go                # CLI entry point
├── internal/
│   ├── cli/                       # CLI subcommand definitions
│   │   ├── root.go
│   │   ├── collection.go          # collection add/remove/list/rename
│   │   ├── index.go               # update / embed / status
│   │   ├── search.go              # search / vsearch / query
│   │   ├── get.go                 # get
│   │   ├── context.go             # context add/remove/list
│   │   └── mcp.go                 # MCP server
│   ├── service/                   # Business logic layer
│   │   ├── collection.go          # Collection management
│   │   ├── indexer.go             # Document indexing (scan + tokenize + FTS5)
│   │   ├── embedder.go            # Vector embedding orchestration
│   │   ├── searcher.go            # Search (BM25 / vector / hybrid)
│   │   ├── fusion.go              # RRF fusion
│   │   └── reranker.go            # Optional reranking
│   ├── store/                     # Data persistence layer
│   │   ├── db.go                  # SQLite connection management
│   │   ├── schema.go              # Schema definition & migration
│   │   ├── document.go            # Document CRUD
│   │   ├── fts.go                 # FTS5 operations
│   │   ├── vector.go              # sqlite-vec operations
│   │   └── collection.go          # Collection persistence
│   ├── tokenizer/                 # Tokenizer abstraction
│   │   ├── tokenizer.go           # Tokenizer interface
│   │   └── gse.go                 # gse implementation
│   ├── embedding/                 # Embedding model abstraction
│   │   ├── provider.go            # EmbeddingProvider interface
│   │   ├── gguf.go                # GGUF local model
│   │   └── model.go               # Model download management
│   ├── chunker/                   # Document chunking
│   │   ├── chunker.go             # Chunker interface
│   │   └── markdown.go            # Markdown-aware chunking
│   └── formatter/                 # Output formatting
│       ├── formatter.go           # Formatter interface
│       ├── text.go                # Colorized terminal
│       ├── json.go
│       ├── markdown.go
│       └── csv.go
├── pkg/                           # Public API for external use
│   └── lmd.go                     # Public Store interface and factory
├── test/                          # Integration tests
│   └── fixtures/                  # Test markdown documents (Chinese + English)
└── go.mod
```

### Layer Dependencies

```
cmd → internal/cli → internal/service → internal/store
                        ↓
              internal/tokenizer (interface)
              internal/embedding (interface)
              internal/chunker (interface)
                        ↓
                     internal/formatter
```

Rules:
- `cmd` depends on `internal/cli` only
- `cli` depends on `service` and `formatter`
- `service` depends on `store`, `tokenizer`, `embedding`, `chunker` interfaces
- `store` depends only on SQLite (mattn/go-sqlite3 + sqlite-vec)
- `pkg/` exports a thin public API that delegates to `service`

## Data Model

### SQLite Schema

```sql
CREATE TABLE collections (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    name            TEXT NOT NULL UNIQUE,
    path            TEXT NOT NULL,
    glob_pattern    TEXT DEFAULT '**/*.md',
    ignore_patterns TEXT,
    created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE path_contexts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    collection  TEXT NOT NULL,
    path        TEXT NOT NULL DEFAULT '',
    context     TEXT NOT NULL,
    UNIQUE(collection, path)
);

CREATE TABLE documents (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    docid       TEXT NOT NULL UNIQUE,
    collection  TEXT NOT NULL,
    path        TEXT NOT NULL,
    title       TEXT,
    body        TEXT NOT NULL,
    hash        TEXT NOT NULL,
    file_size   INTEGER,
    modified_at DATETIME,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE VIRTUAL TABLE documents_fts USING fts5(
    tokens,
    title_tokens,
    content='documents',
    content_rowid='id',
    tokenize='simple'
);

CREATE TABLE chunks (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    doc_id      INTEGER NOT NULL REFERENCES documents(id),
    seq         INTEGER NOT NULL,
    content     TEXT NOT NULL,
    position    INTEGER NOT NULL,
    token_count INTEGER,
    hash        TEXT NOT NULL,
    UNIQUE(doc_id, seq)
);

CREATE VIRTUAL TABLE chunk_vectors USING vec0(
    chunk_id  INTEGER PRIMARY KEY,
    embedding float[1024]
);

CREATE TABLE embed_status (
    chunk_id    INTEGER PRIMARY KEY REFERENCES chunks(id),
    model_name  TEXT NOT NULL,
    embedded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(chunk_id, model_name)
);
```

Key design notes:
- FTS5 uses external content mode referencing `documents` table to avoid data duplication
- Vectors are separated from text chunks, linked by `chunk_id`
- Content hashing enables incremental updates
- `docid` is a 6-character hash for quick document reference

## Core Interfaces

### Tokenizer

```go
type Tokenizer interface {
    Cut(text string) []string
    CutForSearch(text string) []string
    TokenizeToString(text string) string
}
```

### EmbeddingProvider

```go
type EmbeddingProvider interface {
    Embed(ctx context.Context, text string) ([]float32, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
    Dimension() int
    ModelName() string
    Close() error
}
```

### Chunker

```go
type Chunker interface {
    Chunk(title string, body string) ([]Chunk, error)
}

type Chunk struct {
    Content    string
    Position   int
    TokenCount int
}
```

### SearchResult

```go
type SearchResult struct {
    DocID      string
    Collection string
    Path       string
    Title      string
    Score      float64
    Snippet    string
    Context    string
    Line       int
    Sources    []string
}

type SearchOptions struct {
    Query       string
    Collection  string
    Limit       int
    MinScore    float64
    Mode        SearchMode
    Rerank      bool
    ExpandQuery bool
}
```

### Public Store API (pkg/)

```go
type Store interface {
    AddCollection(name string, config CollectionConfig) error
    RemoveCollection(name string) error
    ListCollections() ([]CollectionInfo, error)

    Update(ctx context.Context, opts UpdateOptions) (*UpdateResult, error)
    Embed(ctx context.Context, opts EmbedOptions) (*EmbedResult, error)

    Search(ctx context.Context, opts SearchOptions) ([]SearchResult, error)
    SearchLex(query string, opts LexOptions) ([]SearchResult, error)
    SearchVector(ctx context.Context, query string, opts VecOptions) ([]SearchResult, error)

    Get(pathOrDocID string) (*Document, error)

    AddContext(collection, path, context string) error
    RemoveContext(collection, path string) error
    ListContexts() ([]ContextInfo, error)

    Close() error
}

func CreateStore(opts StoreOptions) (Store, error)
```

## CLI Design

```
lmd [global options] <command> [args]

Global options:
  --index <path>     Database file path (default: ~/.cache/lmd/index.sqlite)
  --help, -h         Show help
  --version          Show version

Commands:

  Collection management:
    collection add <path> --name <name> [--mask <glob>]
    collection remove <name>
    collection list
    collection rename <old> <new>

  Indexing:
    update [--collection <name>]
    embed [--force] [--model <name>]
    status

  Search:
    search <query> [-c <collection>] [-n <num>]
    vsearch <query> [-c <collection>] [-n <num>]
    query <query> [-c <collection>] [-n <num>] [--rerank] [--expand]

  Document retrieval:
    get <path-or-docid> [--full] [-l <num>] [--from <n>]

  Context:
    context add <lmd://collection/path> <description>
    context remove <lmd://collection/path>
    context list

  Agent:
    mcp [--http] [--port <num>]

  Output format options (all search commands):
    --json            JSON output
    --md              Markdown output
    --csv             CSV output
    --full            Show full document content
    --line-numbers    Show line numbers
    --explain         Show score breakdown
    --all             Return all matches
    --min-score <n>   Minimum score threshold
```

Default output example:

```
notes/go-notes.md:42 #a1b2c3
Title: Go Concurrency Patterns
Context: Personal Notes
Score: 93%

This section covers goroutine and channel concurrency patterns...
```

## Indexing Flow

1. Scan collection directories matching glob pattern
2. For each markdown file:
   a. Compute content hash
   b. Compare with stored hash (incremental update)
   c. If changed or new:
      - Parse Markdown, extract title (first `#` heading or filename)
      - Tokenize body with gse → tokenized_body (space-separated)
      - Tokenize title with gse → tokenized_title
      - Write to `documents` table
      - Write to `documents_fts` (tokenized_body + tokenized_title)
      - Delete old chunks if any
      - Markdown-aware chunking → write to `chunks` table
3. Detect deleted files (in DB but not on filesystem) → remove
4. Return stats: indexed, updated, unchanged, removed

## Embedding Flow

1. Query all unembedded chunks (or all if `--force`)
2. Load GGUF embedding model (auto-download on first use)
3. Batch process:
   a. Read chunk content
   b. Format as `"title: {title} | text: {content}"`
   c. Call `EmbeddingProvider.EmbedBatch()`
   d. Write to `chunk_vectors` table (sqlite-vec)
   e. Update `embed_status`
4. Return stats: embedded, skipped, failed

## Search Flows

### BM25 Search (`search` command)

1. Tokenize query with gse → tokenized_query
2. `SELECT * FROM documents_fts WHERE tokens MATCH ? ORDER BY bm25()`
3. Join with `documents` for metadata
4. Extract snippet
5. Normalize score to 0.0-1.0

### Vector Search (`vsearch` command)

1. Call `EmbeddingProvider.Embed(query)` to generate query vector
2. `SELECT chunk_id, distance FROM chunk_vectors WHERE embedding MATCH ?`
3. Join with chunks → documents for metadata
4. Convert distance to similarity: `1 / (1 + distance)`

### Hybrid Search (`query` command)

1. [Optional] Query expansion: generate 1-2 query variants via local GGUF model
2. For each query (original + variants), execute:
   a. BM25 search → ranked list
   b. Vector search → ranked list
3. RRF fusion:
   - Original query results weighted ×2
   - `score = Σ weight × 1/(k + rank + 1)`, k=60
4. Take top-30 candidates
5. [Optional] Reranker scoring
6. Position-aware blending (if reranker enabled):
   - Rank 1-3: 75% RRF / 25% reranker
   - Rank 4-10: 60% RRF / 40% reranker
   - Rank 11+: 40% RRF / 60% reranker
7. Return final results

### RRF Constants

| Parameter | Value | Purpose |
|-----------|-------|---------|
| k | 60 | RRF smoothing constant |
| Top-K | 30 | Candidates sent to reranker |
| OrigWeight | 2 | Original query weight multiplier |

## Markdown-Aware Chunking

Break points scored by boundary type:

| Pattern | Score | Description |
|---------|-------|-------------|
| `# Heading` | 100 | H1 |
| `## Heading` | 90 | H2 |
| `### Heading` | 80 | H3 |
| `` ``` `` | 80 | Code block boundary |
| `---` / `***` | 60 | Horizontal rule |
| Blank line | 20 | Paragraph boundary |

Algorithm:
1. Scan document for all break points with scores
2. When approaching target chunk size (~900 tokens), search a 200-token window before the cutoff
3. Score each break point: `finalScore = baseScore × (1 - (distance/window)^2 × 0.7)`
4. Cut at the highest-scoring break point
5. Code fence protection: no breaks inside code blocks

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Tokenization failure | Fallback to character splitting, log warning |
| Embedding model load failure | `embed` command errors out; `query` falls back to BM25-only |
| sqlite-vec unavailable | Disable vector features, warn user to install |
| File read failure | Skip file, log error, continue with remaining files |
| Collection path missing | Error with clear message |

## Testing Strategy

TDD is mandatory: write tests before implementation.

| Module | Test Focus |
|--------|-----------|
| `tokenizer/gse` | Chinese/English/mixed accuracy, empty string, special characters |
| `chunker/markdown` | Heading boundaries, code block protection, long text handling |
| `store/*` | CRUD operations, FTS5 search, vector search, incremental updates |
| `service/searcher` | BM25 search, vector search, RRF fusion algorithm correctness |
| `service/indexer` | File scanning, hash incremental detection, tokenization+indexing flow |
| `formatter/*` | Output format correctness for each format |
| `pkg/` | Public API integration tests |

Test fixtures: Built-in set of Chinese-English mixed Markdown documents.

## Logging

Use Go standard library `log/slog`. Support `--verbose` flag for debug-level output.

## Module Path

`github.com/lixianmin/lmd`

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/go-ego/gse` | Chinese/English text segmentation |
| `github.com/mattn/go-sqlite3` | SQLite3 with CGo (FTS5 + extension support) |
| `github.com/spf13/cobra` | CLI framework |
| sqlite-vec | Vector similarity search extension |
| llama.cpp (via CGo) | GGUF model loading for embeddings |
