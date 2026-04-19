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
| Embedding model | Ollama HTTP API (sole provider) | No CGo dependency, leverages running Ollama instance |
| Fusion strategy | RRF (Reciprocal Rank Fusion, k=60, 2x weight for primary lists, top-rank bonus) | Rank-based, no score normalization needed between sources |
| Query expansion | HyDE (Hypothetical Document Embedding) via Ollama | Generates hypothetical doc, embeds it as additional search list |
| CLI framework | cobra | Subcommand support, auto-help, shell completion |
| Agent integration | MCP Server + CLI JSON output | Dual access for maximum compatibility |
| Document chunking | Markdown-aware | Respects heading/code block boundaries |
| Collection storage | Single SQLite DB | Convenient cross-collection search |
| Architecture | Client-server daemon (CLI → HTTP → Daemon → Service → DAO) | Background indexing/embedding, persistent process |
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
│   ├── cli/                       # Thin HTTP client wrappers
│   │   ├── root.go
│   │   ├── collection.go          # collection add/remove/list/rename
│   │   ├── search.go              # search / vsearch / query
│   │   ├── get.go                 # get
│   │   ├── daemon.go              # daemon [--detach]
│   │   └── memory.go              # memory add/search
│   ├── daemon/                    # Daemon server
│   │   ├── daemon.go              # Lifecycle (start, stop, background goroutines)
│   │   ├── server.go              # HTTP API server
│   │   ├── routes.go              # Route handlers
│   │   └── client.go              # CLI client that talks to daemon
│   ├── config/                    # Configuration
│   │   └── config.go              # YAML loading, defaults, save
│   ├── dao/                       # Data persistence layer
│   │   ├── db.go                  # SQLite connection management
│   │   ├── schema.go              # Schema definition
│   │   ├── document.go            # Document CRUD
│   │   ├── chunks_fts.go          # FTS5 operations
│   │   ├── chunks_vec.go          # sqlite-vec operations
│   │   ├── collection.go          # Collection persistence
│   │   ├── stats.go               # Count queries
│   │   └── memory.go              # Memory CRUD
│   ├── service/                   # Business logic layer
│   │   ├── indexer.go             # Document indexing
│   │   ├── embedder.go            # Vector embedding
│   │   ├── searcher.go            # Search (BM25 / vector / hybrid)
│   │   ├── fusion.go              # RRF fusion
│   │   └── memory.go              # Memory operations
│   ├── tokenizer/                 # Tokenizer
│   │   └── gse.go                 # gse implementation
│   ├── embedding/                 # Embedding
│   │   ├── provider.go            # EmbeddingProvider interface
│   │   └── ollama.go              # Ollama HTTP provider
│   ├── chunker/                   # Document chunking
│   │   ├── chunker.go             # Chunker interface
│   │   └── markdown.go            # Sliding window chunker
│   ├── formatter/                 # Output formatting
│   │   ├── formatter.go           # Formatter interface + SearchHit
│   │   ├── text.go
│   │   ├── json.go
│   │   ├── markdown.go
│   │   └── csv.go
│   └── mcp/                       # MCP protocol handler (served by daemon)
├── pkg/                           # Public API for external use
│   └── lmd.go                     # Public Store interface and factory
├── test/                          # Integration tests
│   └── fixtures/                  # Test markdown documents (Chinese + English)
└── go.mod
```

### Layer Dependencies

```
CLI → HTTP JSON → Daemon → Service → DAO
MCP agent → HTTP/stdio → Daemon → Service → DAO
```

Rules:
- `cmd` depends on `internal/cli` only
- `cli` depends on `daemon/client` for HTTP communication
- `daemon` depends on `service`, `config`, and `mcp`
- `service` depends on `dao`, `tokenizer`, `embedding`, `chunker` interfaces
- `dao` depends only on SQLite (mattn/go-sqlite3 + sqlite-vec)
- `config` is loaded by `daemon` and `cli` (for daemon address)
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
    created_at      DATETIME DEFAULT (DATETIME('now', '+8 hours')),
    updated_at      DATETIME DEFAULT (DATETIME('now', '+8 hours'))
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

CREATE VIRTUAL TABLE chunks_fts USING fts5(
    content,
    content='chunks',
    content_rowid='id',
    tokenize='porter unicode61'
);

CREATE VIRTUAL TABLE chunks_vec USING vec0(
    chunk_id  INTEGER PRIMARY KEY,
    embedding float[1024] distance_metric=cosine
);
```

> **Planned tables (not yet implemented):**
> - `embed_status` — track which chunks have been embedded and by which model
> - `path_contexts` — descriptive metadata for collections/paths
> - `_meta` — schema version tracking for migrations

Key design notes:
- FTS5 uses external content mode referencing `chunks` table (chunk-level, not document-level)
- `chunks_fts` uses `porter unicode61` tokenizer; gse pre-tokenizes Chinese before insertion
- `chunks_vec` uses cosine distance metric via sqlite-vec
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
    EmbedQuery(ctx context.Context, text string) ([]float32, error)
    EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
    Dimension() int
    ModelName() string
    Close() error
}
```

> Note: `EmbedQuery` currently delegates to `Embed` (no Instruct prefix).

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

### SearchHit

```go
type SearchHit struct {
    ChunkId    int64
    DocId      string
    Collection string
    Path       string
    Title      string
    Score      float64
    Snippet    string
    Line       int
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
    FileSize   int64
    ModifiedAt time.Time
}

type Store interface {
    AddCollection(name string, config CollectionConfig) error
    RemoveCollection(name string) error
    ListCollections() ([]CollectionInfo, error)

    Update(ctx context.Context, opts UpdateOptions) (*UpdateResult, error)
    Embed(ctx context.Context, opts EmbedOptions) (*EmbedResult, error)

    Search(ctx context.Context, query string, collection string, limit int) ([]SearchHit, error)
    SearchLex(query string, opts LexOptions) ([]SearchHit, error)
    SearchVector(ctx context.Context, query string, opts VecOptions) ([]SearchHit, error)

    Get(pathOrDocID string) (*Document, error)

    Close() error
}

func CreateStore(opts StoreOptions) (Store, error)
```

## CLI Design

```
lmd [global options] <command> [args]

Global options:
  --verbose          Enable debug-level logging
  --help, -h         Show help
  --version          Show version

Commands:

  Daemon:
    daemon [--detach]                  Start daemon (foreground, or --detach for background)

  Collection management:
    collection add <path> --name <name> [--mask <glob>]
    collection remove <name>
    collection list
    collection rename <old> <new>

  Search:
    search <query> [-c <collection>] [-n <num>]
    vsearch <query> [-c <collection>] [-n <num>]
    query <query> [-c <collection>] [-n <num>]

  Document retrieval:
    get <path-or-docid> [--full] [-l <num>] [--from <n>]

  Memory (agent):
    memory add <content> [--type fact|episode|relation]
    memory search <query> [--type <type>] [-n <num>]

  Advanced (daemon background tasks):
    update [--collection <name>]
    embed [--force]
    rebuild [--collection <name>]
    status

  Output format options (all search commands):
    --json            JSON output
    --md              Markdown output
    --csv             CSV output
    --full            Show full document content
    --min-score <n>   Minimum score threshold
```

> **Note**: `update`, `embed`, and `rebuild` are advanced commands for manual triggers. The daemon handles indexing and embedding automatically in the background. MCP is served by the daemon, not as a separate CLI command.

Default output example:

```
notes/go-notes.md:42 #a1b2c3
Title: Go Concurrency Patterns
Context: Personal Notes
Score: 93%

This section covers goroutine and channel concurrency patterns...
```

## MCP Server

LMD exposes an MCP (Model Context Protocol) server for agent integration, speaking stdio.

### Transport Mode

- **stdio** (default and only): Launched as subprocess by MCP client, communicates via stdin/stdout

### MCP Tools Exposed

| Tool | Description | Parameters |
|------|-------------|------------|
| `search` | BM25 keyword search | `query` (string), `collection` (optional string), `limit` (int, default 5) |
| `search_vector` | Vector semantic search | `query` (string), `collection` (optional string), `limit` (int) |
| `query` | Hybrid search (BM25 + vector + optional HyDE) | `query` (string), `collection` (optional string), `limit` (int, default 5) |
| `memory_add` | Add agent memory | `content` (string), `type` ("fact"\|"episode"\|"relation", default "episode") |
| `memory_search` | Search agent memories | `query` (string), `limit` (int, default 10), `type` (optional string) |
| `get` | Retrieve document by path or docid | `path_or_docid` (string), `full` (bool, default false) |
| `status` | Index health and collection info | (none) |
| `list_collections` | List all collections with stats | (none) |

### Implementation

The MCP server (`internal/mcp/`) is served by the daemon as an HTTP endpoint, not as a separate CLI subprocess. It delegates to the `Store` interface and maps tool calls to Store method invocations, formatting results as structured JSON.

## Reranker

> **Status: Planned, deferred to Phase 3 (after daemon architecture is complete)**

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

## HyDE Query Expansion

> **Status: Planned, not yet implemented**

### Approach

Hypothetical Document Embedding (HyDE): instead of generating alternative query phrasings, generate a hypothetical document that would answer the query, embed it, and use it as an additional vector search list in RRF fusion.

### Model

Uses Ollama with a general-purpose generative model (default `qwen3:0.6b-q8_0`, configurable to any Ollama model). This is separate from the embedding model.

### Behavior

When `query` is called with HyDE enabled:
1. Generate hypothetical document via Ollama: "Given query '{q}', write a short passage that would answer this query"
2. Embed the hypothetical document using the embedding provider
3. Add it as an additional vector search list in RRF fusion (weight 1.0, not 2.0)
4. This improves recall for queries where the user's phrasing differs from the document's

Config:
```yaml
hyde:
  enabled: true
  model: qwen3:0.6b-q8_0
```

- Only activated when `hyde.enabled` is true in config
- If model fails, search proceeds without HyDE expansion (graceful degradation)

## Context System

> **Status: Planned, not yet implemented**

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

## Embedding Integration

### Approach

LMD communicates with embedding models via Ollama HTTP API, avoiding any CGo dependency. Ollama is the sole embedding provider — no llama-server subprocess.

### Provider

**Ollama HTTP API**: Connects to a running Ollama instance at `http://localhost:11434/api/embed`. Requires Ollama to be installed and running with the embedding model loaded. Parameters are read from `config.Cfg.Embedding.Ollama`.

### Configuration

```yaml
embedding:
  provider: ollama
  ollama:
    url: http://localhost:11434
    model: qwen3-embedding:0.6b-q8_0
    keep_alive: 30m
  batch_size: 8
  truncation: 800
```

### Go Implementation

`internal/embedding/ollama.go` implements `OllamaProvider` (the `EmbeddingProvider` interface) using standard Go `net/http` client. No CGo required.

## Schema Management

> **Status: No migration system currently implemented.** Schema is created via `dao/schema.go` on first use. Migrations will be added when schema changes are needed.

## Concurrency

- SQLite opened in WAL (Write-Ahead Logging) mode to allow concurrent reads during writes
- No advisory lock currently implemented; concurrent write operations should be avoided by the caller
- Search operations can run concurrently with each other
- Model inference (embedding) is serialized per provider instance

## Score Normalization

| Source | Raw Score | Normalization | Range |
|--------|-----------|---------------|-------|
| FTS5 (BM25) | SQLite `rank` (negative) | `abs(score) / (1.0 + abs(score))` | 0.0-1.0 |
| Vector | Cosine distance (0+) | `1.0 - distance` | 0.0-1.0 |

BM25 normalization uses a sigmoid-like formula that naturally maps to 0-1 without needing a reference score. Vector scoring uses cosine distance directly (lower distance = higher similarity).

> **Note**: Hybrid `query` uses RRF which operates on ranks, not raw scores. These normalization formulas apply only to standalone `search` (BM25) and `vsearch` (vector) commands.

## Indexing Flow

1. Scan collection directories matching glob pattern
2. For each markdown file:
   a. Compute content hash
   b. Compare with stored hash (incremental update)
   c. If changed or new:
      - Parse Markdown, extract title (first `#` heading or filename)
      - Write to `documents` table
      - Delete old chunks if any
      - Markdown-aware chunking → write to `chunks` table
      - For each chunk, insert FTS entry into `chunks_fts` (gse pre-tokenized content)
3. Detect deleted files (in DB but not on filesystem) → remove
4. Return stats: indexed, updated, unchanged, removed

## Embedding Flow

1. Query all unembedded chunks (or all if `--force`)
2. Batch process (batch size = 8):
   a. Read chunk content (truncated to 800 runes if longer)
   b. Call `EmbeddingProvider.EmbedBatch()` via Ollama HTTP API
   c. Write to `chunks_vec` table (sqlite-vec)
3. Return stats: embedded, skipped, failed

## Search Flows

### BM25 Search (`search` command)

1. Tokenize query with gse → tokenized_query
2. `SELECT * FROM chunks_fts WHERE content MATCH ? ORDER BY rank`
3. Join with `chunks` → `documents` for metadata
4. Extract snippet
5. Normalize score: `score = abs(score) / (1.0 + abs(score))`

### Vector Search (`vsearch` command)

1. Call `EmbeddingProvider.EmbedQuery(query)` to generate query vector
2. `SELECT chunk_id, distance FROM chunks_vec WHERE embedding MATCH ?`
3. Join with chunks → documents for metadata
4. Convert distance to similarity: `1.0 - distance`

### Hybrid Search (`query` command)

Uses Reciprocal Rank Fusion (RRF):
1. Execute BM25 search → ranked list
2. Execute vector search → ranked list
3. (Optional) Generate HyDE document, embed it, execute additional vector search → ranked list
4. Fuse via RRF:
   ```
   rrfScore(c) = SUM( weight_i / (k + rank_i + 1) ) + topRankBonus
   k = 60
   First 2 lists (BM25 + vector): weight = 2.0
   Additional lists (HyDE variants): weight = 1.0
   topRankBonus = +0.05 if best rank is #1, +0.02 if #2-#3
   ```
5. Group by ChunkId, preserving multiple chunks from same document
6. Sort by RRF score descending
7. Return final results

## MMR (Maximal Marginal Relevance)

> **Status: Planned, not yet implemented**

After fusion, MMR would promote result diversity and reduce redundancy.

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
| `dao/*` | CRUD operations, FTS5 search, vector search, incremental updates |
| `service/searcher` | BM25 search, vector search, RRF fusion correctness |
| `service/indexer` | File scanning, hash incremental detection, tokenization+indexing flow |
| `formatter/*` | Output format correctness for each format |
| `pkg/` | Public API integration tests |

Test fixtures: Built-in set of Chinese-English mixed Markdown documents.

## Logging

Use `github.com/lixianmin/logo` for logging. Default level is Info; `--verbose` flag sets level to Debug. Support `logo.NewRollingFileHook` for file logging when needed.

## Timezone Handling

All timestamps in the database (`created_at`, `updated_at`, `modified_at`, `embedded_at`) are stored in **GMT+8 (CST)** format using `DATETIME('now', '+8 hours')` as the default value in SQLite.

In Go code, all time values are read/written in Asia/Shanghai timezone. The `time.LoadLocation("Asia/Shanghai")` is used for formatting and parsing.

## Module Path

`github.com/lixianmin/lmd`

## Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/go-ego/gse` | Chinese/English text segmentation |
| `github.com/lixianmin/logo` | Lightweight logging library |
| `github.com/mattn/go-sqlite3` | SQLite3 with CGo (FTS5 + extension support) |
| `github.com/spf13/cobra` | CLI framework |
| sqlite-vec | Vector similarity search extension |

## Config System

Configuration is centralized in `~/.config/lmd/config.yaml`, auto-generated on first run if missing.

```yaml
daemon:
  port: 18200
  idle_timeout: 30m
  index_poll_interval: 60s

embedding:
  provider: ollama
  ollama:
    url: http://localhost:11434
    model: qwen3-embedding:0.6b-q8_0
    keep_alive: 30m
  batch_size: 8
  truncation: 800

vector:
  dimensions: 1024
  distance_metric: cosine

database:
  path: ~/.cache/lmd/index.sqlite
```

Implemented in `internal/config/config.go` with `Load()`, `SaveDefault()`, and `DefaultConfig()`. See daemon spec §3 for full details.

## Memory Layer

LMD doubles as a memory layer for AI agents. Memories are stored in an independent `memories` table (sharing the same SQLite DB and embedding provider), with no explicit delete — old memories decay naturally.

### Memory Types and Decay

| Type | Description | Half-life |
|------|-------------|-----------|
| `fact` | Factual knowledge | Never (score unchanged) |
| `episode` | Events/experiences | 15 days |
| `relation` | Preferences/associations | 180 days |

Time decay is applied at query time: `final_score = raw_score × 0.5^(age_days / half_life)`.

### Operations

- **memory_add**: Insert memory, auto-embed, insert FTS entry. Returns `{id, type, created_at}`.
- **memory_search**: Search memories with time-decay scoring. Returns `[{id, content, type, score, created_at}]`.

See daemon spec §11 for full schema and design details.

## Implementation Phasing

The spec covers multiple subsystems. Recommended implementation order:

**Phase 1 - Foundation**: DAO layer (SQLite schema, CRUD) + Tokenizer (gse) + BM25 search + basic CLI (collection, search, get)
**Phase 2 - Daemon + Config**: Config system (`internal/config/`) + Daemon architecture (`internal/daemon/`) + CLI→HTTP migration + background indexer/embedder
**Phase 3 - Vector**: Ollama-only embedding + chunking + vector storage (sqlite-vec) + vsearch command
**Phase 4 - Hybrid**: RRF fusion + HyDE query expansion + query command
**Phase 5 - Integration**: MCP in daemon + memory layer + context system + public API polish + output formatters + reranker
