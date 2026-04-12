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
| Keyword search | gse pre-tokenize + FTS5 unicode61 | Splits on whitespace after gse pre-tokenization, leverages mature FTS5 BM25 |
| Embedding model | Qwen3-Embedding-0.6B (GGUF) | 119 languages including CJK, MTEB top-ranked |
| Fusion strategy | RRF priority + optional reranking | Fast by default, quality when needed |
| Query expansion | Optional | User-controlled complexity |
| CLI framework | cobra | Subcommand support, auto-help, shell completion |
| Agent integration | MCP Server + CLI JSON output | Dual access for maximum compatibility |
| Document chunking | Markdown-aware | Respects heading/code block boundaries |
| Collection storage | Single SQLite DB | Convenient cross-collection search |
| Architecture | Layered pipeline (CLI → Service → Store) | Clear separation, testable, extensible |
| MMR diversity | Applied after fusion | Reduces redundant similar results |
| Timezone | GMT+8 (CST) for all timestamps | User in East Asia, avoid UTC confusion |

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
    created_at  DATETIME DEFAULT (DATETIME('now', '+8 hours')),
    updated_at  DATETIME DEFAULT (DATETIME('now', '+8 hours'))
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
    created_at  DATETIME DEFAULT (DATETIME('now', '+8 hours')),
    updated_at  DATETIME DEFAULT (DATETIME('now', '+8 hours'))
);

CREATE VIRTUAL TABLE documents_fts USING fts5(
    tokens,
    title_tokens,
    content='documents',
    content_rowid='id',
    tokenize='unicode61'
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
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    chunk_id    INTEGER NOT NULL REFERENCES chunks(id),
    model_name  TEXT NOT NULL,
    embedded_at DATETIME DEFAULT (DATETIME('now', '+8 hours')),
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
type CollectionConfig struct {
    Path           string   // Local directory absolute path
    GlobPattern    string   // File matching pattern (default "**/*.md")
    IgnorePatterns []string // Patterns to ignore
}

type CollectionInfo struct {
    Name         string
    Path         string
    GlobPattern  string
    DocCount     int
    ActiveCount  int
    LastModified time.Time
}

type StoreOptions struct {
    DBPath      string          // Required: path to SQLite database file
    Config      *InlineConfig   // Optional: inline collection configuration
    ConfigPath  string          // Optional: path to YAML config file
}

// YAML config file format (when ConfigPath is used):
// collections:
//   docs:
//     path: /path/to/docs
//     glob_pattern: "**/*.md"
//   notes:
//     path: /path/to/notes

type InlineConfig struct {
    Collections map[string]CollectionConfig
}

type UpdateOptions struct {
    Collections []string // Empty = all collections
    OnProgress  func(UpdateProgress)
}

type UpdateProgress struct {
    Collection string
    File       string
    Current    int
    Total      int
}

type UpdateResult struct {
    Collections int
    Indexed     int
    Updated     int
    Unchanged   int
    Removed     int
}

type EmbedOptions struct {
    Force      bool // Re-embed everything
    ModelName  string
    OnProgress func(EmbedProgress)
}

type EmbedProgress struct {
    Current    int
    Total      int
    Collection string
}

type EmbedResult struct {
    Embedded int
    Skipped  int
    Failed   int
}

type LexOptions struct {
    Collection string
    Limit      int
    MinScore   float64
}

type VecOptions struct {
    Collection string
    Limit      int
    MinScore   float64
}

type Document struct {
    DocID      string
    Collection string
    Path       string
    Title      string
    Body       string
    Context    string
    FileSize   int64
    ModifiedAt time.Time
}

type ContextInfo struct {
    Collection string
    Path       string
    Context    string
}

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
  --verbose          Enable debug-level logging
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

## MCP Server

LMD exposes an MCP (Model Context Protocol) server for agent integration, speaking stdio by default with optional HTTP transport.

### Transport Modes

- **stdio** (default): Launched as subprocess by MCP client, communicates via stdin/stdout
- **HTTP**: Long-lived server at `http://localhost:8181/mcp` (Streamable HTTP, stateless JSON responses)

### MCP Tools Exposed

| Tool | Description | Parameters |
|------|-------------|------------|
| `search` | Hybrid search (BM25 + vector + optional rerank) | `query` (string), `collection` (optional string), `limit` (int, default 5), `min_score` (float), `rerank` (bool), `expand` (bool) |
| `search_lex` | BM25 keyword search only | `query` (string), `collection` (optional string), `limit` (int) |
| `search_vector` | Semantic vector search only | `query` (string), `collection` (optional string), `limit` (int) |
| `get` | Retrieve document by path or docid | `path_or_docid` (string), `full` (bool, default false) |
| `status` | Index health and collection info | (none) |
| `list_collections` | List all collections with stats | (none) |

### Implementation

The MCP server (`internal/cli/mcp.go`) delegates to the `Store` interface. It uses a lightweight MCP protocol handler that maps tool calls to Store method invocations and formats results as structured JSON.

## Reranker

### Model

Uses Qwen3-Reranker-0.6B (GGUF, ~640MB), a cross-encoder model that scores document-query relevance.

### Interface

```go
type Reranker interface {
    Rerank(ctx context.Context, query string, documents []RerankItem) ([]RerankResult, error)
    Close() error
}

type RerankItem struct {
    DocID   string
    Title   string
    Content string
}

type RerankResult struct {
    DocID string
    Score float64 // 0.0-1.0
}
```

### Scoring Method

The reranker uses a yes/no classification with logprob confidence: for each candidate document, the model evaluates "Is this document relevant to the query?" and extracts the confidence probability from logprobs. The raw 0-10 rating is normalized to 0.0-1.0 by dividing by 10.

### Position-Aware Blending Rationale

Top-ranked RRF results are likely strong exact matches — the reranker is given less weight (25%) to preserve them. Lower-ranked results benefit more from the reranker's deeper understanding (60%), as pure retrieval signals are weaker.

## Query Expansion

### Model

Uses a separate small generative model (Qwen2.5-1.5B-Instruct GGUF, ~1GB), not the embedding model. The embedding model (Qwen3-Embedding) only produces vectors and cannot generate text.

### Prompt

```
Given the following search query, generate 2 alternative phrasings that would match
different ways of expressing the same information. Keep the alternatives concise.
Return one alternative per line, no numbering.

Query: {query}
```

### Behavior

- Only activated when `--expand` flag is passed or `ExpandQuery: true` in API
- Model is lazy-loaded on first use, stays in memory for subsequent queries
- If model fails to load, search proceeds without expansion (graceful degradation)

## Context System

### Purpose

Context adds descriptive metadata to collections and paths within collections. When search results are returned, the matching context is included in the output. This helps agents and users understand *why* a document matched and *what domain* it belongs to.

### URI Scheme

`lmd://collection_name/path/within/collection`

Examples:
- `lmd://notes` → context for the entire "notes" collection
- `lmd://notes/work` → context for the "work" subfolder within "notes"
- `lmd://docs/api` → context for the "api" subfolder within "docs"

### Application During Search

When returning search results, the system finds the most specific matching context for each result's path. For a document at `notes/work/project-a.md`, it checks:
1. `lmd://notes/work/project-a` (exact match)
2. `lmd://notes/work` (parent)
3. `lmd://notes` (collection-level)
4. Global context (path="" in any collection)

The most specific match is used. If no context is found, the field is empty.

## Document Retrieval Flow (`get` command)

1. Parse input: determine if it's a docid (starts with `#`) or a path
2. If docid: `SELECT * FROM documents WHERE docid = ?`
3. If path: `SELECT * FROM documents WHERE collection = ? AND path = ?`
   - If no exact match, attempt prefix-based fuzzy matching and suggest similar files
4. If `--from <n>` is specified, return body starting from line n
5. If `-l <num>` is specified, limit output to num lines
6. If `--full` is specified, return entire document body; otherwise return a snippet summary
7. Attach any matching context from `path_contexts`

## llama.cpp Integration

### Approach

LMD uses CGo bindings to link against llama.cpp's shared library for loading GGUF models. This is the same approach Ollama uses (which is also written in Go).

### Build Requirements

- C compiler (gcc/clang)
- llama.cpp shared library (`libllama.so` / `libllama.dylib`)
- CGo enabled (`CGO_ENABLED=1`)

### Model Loading

Models are downloaded from HuggingFace on first use, cached in `~/.cache/lmd/models/`. The download uses the HF repo ID + filename pattern:
- Embedding: `hf:Qwen/Qwen3-Embedding-0.6B-GGUF/Qwen3-Embedding-0.6B-Q8_0.gguf`
- Reranker: `hf:ggml-org/Qwen3-Reranker-0.6B-Q8_0-GGUF/qwen3-reranker-0.6b-q8_0.gguf`
- Expansion: `hf:Qwen/Qwen2.5-1.5B-Instruct-GGUF/qwen2.5-1.5b-instruct-q4_k_m.gguf`

### Go Binding

Use `github.com/ngx Tahoe/go-llama.cpp` or a similar Go CGo wrapper. If no mature binding exists, write a minimal CGo wrapper in `internal/embedding/llama/` that exposes:
- `llama_load_model(path) → *model`
- `llama_embed(model, texts) → [][]float32`
- `llama_free_model(model)`

## Schema Migration

Schema version is tracked in a `_meta` table:

```sql
CREATE TABLE _meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
-- Initial row: INSERT INTO _meta (key, value) VALUES ('schema_version', '1');
```

On startup, `store/schema.go` reads the current version and applies migrations sequentially. Each migration is a Go function that executes SQL statements within a transaction. Migrations are one-way (no rollback support in v1).

```go
var migrations = []Migration{
    {Version: 1, Up: migrateV1},
}

func migrateV1(db *sql.DB) error {
    // Create all initial tables
}
```

## Concurrency

- SQLite opened in WAL (Write-Ahead Logging) mode to allow concurrent reads during writes
- `update` and `embed` operations acquire an advisory lock to prevent concurrent indexing
- Search operations can run concurrently with each other
- Model inference (embedding/reranker/expansion) is serialized per model instance (GPU/CPU bound)

## Score Normalization

| Source | Raw Score | Normalization | Range |
|--------|-----------|---------------|-------|
| FTS5 (BM25) | SQLite `bm25()` (negative) | `min(abs(score) / max_score, 1.0)` where `max_score` is the top result's score | 0.0-1.0 |
| Vector | Cosine distance (0+) | `1 / (1 + distance)` | 0.0-1.0 |
| Reranker | 0-10 rating | `score / 10` | 0.0-1.0 |

BM25 normalization uses the top result as denominator (rank-based normalization). This avoids needing to know the theoretical maximum BM25 score.

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
5. Normalize score: `score = min(abs(score) / top_score, 1.0)` where `top_score` is the highest BM25 score in results

### Vector Search (`vsearch` command)

1. Call `EmbeddingProvider.Embed(query)` to generate query vector
2. `SELECT chunk_id, distance FROM chunk_vectors WHERE embedding MATCH ?`
3. Join with chunks → documents for metadata
4. Convert distance to similarity: `1 / (1 + distance)`

### Hybrid Search (`query` command)

1. [Optional] Query expansion: generate 1-2 query variants via Qwen2.5-1.5B-Instruct (see Query Expansion section)
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

## MMR (Maximal Marginal Relevance)

After RRF fusion and optional reranking, MMR is applied to promote result diversity and reduce redundancy.

### Algorithm

For each candidate position, MMR selects the document that maximizes:

```
MMR(d) = λ × rel(d) - (1-λ) × max[sim(d, d') for d' in already-selected]
```

Where:
- `rel(d)` = document's fused score (RRF or blended)
- `sim(d, d')` = cosine similarity between document embeddings or chunk content overlap
- `λ` = relevance-diversity trade-off (default: 0.7, higher = more relevance, lower = more diversity)

### Implementation

- Use chunk content TF-IDF similarity (not embedding vectors) for efficiency
- λ configurable via `--mmr-lambda` flag (default 0.7)
- Applied to top-30 candidates before final ranking
- Reduces cases where multiple chunks from the same document or very similar documents dominate results

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
| Model download failure | Error with retry suggestion; partial downloads are cleaned up |
| Model download interrupted | Clean up partial file, error out, user re-runs command |

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

## Timezone Handling

All timestamps in the database (`created_at`, `updated_at`, `modified_at`, `embedded_at`) are stored in **GMT+8 (CST)** format using `DATETIME('now', '+8 hours')` as the default value in SQLite.

In Go code, all time values are read/written in Asia/Shanghai timezone. The `time.LoadLocation("Asia/Shanghai")` is used for formatting and parsing.

## Module Path

`github.com/lixianmin/lmd`

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/go-ego/gse` | Chinese/English text segmentation |
| `github.com/mattn/go-sqlite3` | SQLite3 with CGo (FTS5 + extension support) |
| `github.com/spf13/cobra` | CLI framework |
| sqlite-vec | Vector similarity search extension |
| llama.cpp (via CGo) | GGUF model loading for embeddings, reranking, query expansion |

## Implementation Phasing

The spec covers multiple subsystems. Recommended implementation order:

**Phase 1 - Foundation**: Store layer (SQLite schema, CRUD) + Tokenizer (gse) + BM25 search + basic CLI (collection, update, search, get)
**Phase 2 - Vector**: Embedding provider (GGUF) + chunking + vector storage (sqlite-vec) + vsearch command
**Phase 3 - Hybrid**: RRF fusion + query command + reranker + query expansion
**Phase 4 - Integration**: MCP server + context system + public API polish + output formatters
