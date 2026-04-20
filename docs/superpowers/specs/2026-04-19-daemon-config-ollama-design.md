# LMD Daemon Architecture + Config System + Ollama-only Embedding

## Overview

Transform LMD from a per-command CLI tool into a client-server architecture:
- **Daemon** (`lmd daemon` or auto-started): a persistent background process that handles indexing, embedding, search, collection management, and MCP serving
- **CLI** (`lmd <command>`): a thin client that communicates with the daemon via HTTP JSON
- **Config** (`~/.config/lmd/config.yaml`): centralized configuration for all parameters

This addresses:
1. Embedding is too slow to run on every search — daemon does it in the background
2. No configuration file — all parameters are hardcoded
3. Unnecessary llama-server subprocess — Ollama already handles model serving
4. `collection add` shows 0 docs — daemon auto-indexes on startup
5. MCP/CLI search semantics mismatch — unified under daemon

## Design Principles

1. **Local-first**: Use Ollama for embedding, no cloud services
2. **Free-first**: Everything runs locally, no API costs
3. **One thing per command**: CLI is a client, daemon is the server
4. **Don't reinvent the wheel**: Use Ollama for model serving, not llama-server subprocess
5. **Zero-config by default**: Auto-generate config, auto-start daemon, auto-detect Ollama

## Architecture

```
┌─────────────┐    HTTP JSON    ┌──────────────────────────────┐
│  lmd CLI    │ ◄──────────────► │  lmd daemon                  │
│  (thin)     │    localhost:port│                              │
└─────────────┘                  │  ┌─ HTTP API server          │
                                 │  ├─ MCP server (HTTP)        │
┌─────────────┐    HTTP/MCP      │  ├─ Indexer (poll 60s)       │
│  AI Agent   │ ◄──────────────► │  ├─ Embedder (background)    │
│  (Cursor)   │    stdio/HTTP    │  ├─ Searcher                 │
└─────────────┘                  │  ├─ Collection manager       │
                                 │  ├─ Memory layer (agent)     │
                                 │                              │
                                 │  Config: ~/.config/lmd/      │
                                 │  DB:     ~/.cache/lmd/        │
                                 └──────────────────────────────┘
```

## 1. Daemon

### Lifecycle

1. **Auto-start**: When any `lmd` CLI command runs, it checks if daemon is alive (HTTP GET `/health`). If not, it starts the daemon in background and waits for it to be ready.
2. **Run**: Daemon loads config, opens SQLite, starts HTTP server, starts background indexer/embedder goroutines.
3. **Shutdown**: Daemon catches SIGINT/SIGTERM, gracefully closes SQLite, stops goroutines, exits. Or auto-shutdown after configurable idle timeout.

### PID and Port

- PID file: `~/.cache/lmd/daemon.pid`
- Default port: `12345` (configurable in config.yaml)
- Health check: `GET /health` returns `{"status": "ok"}`

### HTTP API Endpoints

| Method | Path | Description | Request | Response |
|--------|------|-------------|---------|----------|
| GET | `/health` | Health check | - | `{"status":"ok"}` |
| POST | `/search` | BM25 keyword search | `{"query":"...","collection":"...","limit":5,"min_score":0,"format":"text","json":false}` | Search results |
| POST | `/vsearch` | Vector semantic search | `{"query":"...","collection":"...","limit":5,"min_score":0.3}` | Search results |
| POST | `/query` | Hybrid search (BM25 + vector + optional HyDE) | `{"query":"...","collection":"...","limit":5,"min_score":0}` | Search results |
| POST | `/get` | Get document | `{"path":"...","full":false,"from":0,"lines":0}` | Document |
| GET | `/status` | Index status | - | Status info |
| POST | `/collection/add` | Add collection | `{"path":"...","name":"...","mask":"**/*.md"}` | Result |
| POST | `/collection/remove` | Remove collection | `{"name":"..."}` | Result |
| GET | `/collection/list` | List collections | - | Collection list |
| POST | `/collection/rename` | Rename collection | `{"old":"...","new":"..."}` | Result |
| POST | `/rebuild` | Full rebuild | - | Result |
| POST | `/memory/add` | Add agent memory | `{"content":"...","type":"episode"}` | `{id, type, created_at}` |
| POST | `/memory/search` | Search agent memories | `{"query":"...","limit":10,"type":""}` | `[{id, content, type, score, created_at}]` |
| POST | `/mcp` | MCP JSON-RPC endpoint | JSON-RPC request | JSON-RPC response |

### Background Goroutines

#### Index Poller
- Every 30 seconds (configurable via `daemon.index_poll_interval`), scan all collections
- Compare file modification timestamps with stored values
- Index changed/new files, remove deleted files
- Uses timestamp fast-path (os.Stat only), falls back to SHA-256 for changed files

#### Embed Worker
- Periodically (10s ticker) embeds unembedded chunks AND memories
- Embeds chunks in batches of `config.embedding.batch_size`
- Runs in a single goroutine (Ollama handles parallelism internally)
- Logs progress to stderr/daemon log

### Auto-shutdown

- Configurable idle timeout (default: 30 minutes)
- Resets on any API request
- When idle timeout expires: close DB, exit
- Next CLI command will auto-start daemon again

## 2. CLI Client

### Behavior Change

Currently: each CLI command directly calls service/dao layers.
New: each CLI command sends HTTP request to daemon, prints response.

### Auto-start Logic (in `root.go` PersistentPreRunE)

```
1. config.Load() — load or generate config
2. Check daemon alive: GET http://localhost:{port}/health
   - Alive: proceed with command
   - Not alive:
      a. Start daemon: exec.Command(os.Args[0], "daemon", "start")
     b. Wait for ready: poll /health every 100ms, timeout 30s
     c. Proceed with command
3. Execute command via HTTP API
```

### `lmd daemon` Command

New subcommands: `lmd daemon start` / `lmd daemon stop`
- `start`: run in foreground (for debugging / manual start / background fork)
- `stop`: send SIGTERM to daemon process
- Auto-start uses `exec.Command(os.Args[0], "daemon", "start")` with stdout/stderr redirected to `~/.cache/lmd/logs/daemon.stderr.log`

## 3. Config File

**Location**: `~/.config/lmd/config.yaml`
**Auto-generated**: On first run if missing

```yaml
# LMD Configuration
# Auto-generated on first run. Edit as needed.
# Restart daemon to apply changes: kill $(cat ~/.cache/lmd/daemon.pid)

daemon:
  port: 18200
  idle_timeout: 30m
  index_poll_interval: 30s

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

hyde:
  enabled: true
  model: qwen3:0.6b-q8_0
```

### Config Package

**Location**: `internal/config/config.go`

```go
type Config struct {
    Daemon    DaemonConfig    `yaml:"daemon"`
    Embedding EmbeddingConfig `yaml:"embedding"`
    Vector    VectorConfig    `yaml:"vector"`
    Database  DatabaseConfig  `yaml:"database"`
    HyDE      HyDEConfig      `yaml:"hyde"`
}
```

- `Load() (*Config, error)` — read file, return defaults if missing
- `SaveDefault() error` — write default config
- `DefaultConfig() *Config` — hardcoded defaults
- Global `var Cfg *Config`

## 4. Remove llama-server Subprocess

Delete from `internal/embedding/`:
- All llama-server subprocess management (~200 lines)
- HuggingFace model download logic
- PID/last-active file management
- Rename `GGUFProvider` → `OllamaProvider`
- File `gguf.go` → `ollama.go`

`OllamaProvider` reads parameters from `config.Cfg.Embedding.Ollama`.

## 5. Fusion: Switch to RRF

Reference: QMD `src/store.ts:3346-3389`

Replace current weighted linear combination with Reciprocal Rank Fusion:

```
For each chunk c:
  rrfScore(c) = SUM( weight_i / (k + rank_i + 1) ) + topRankBonus

Where:
  k = 60
  First 2 lists (original BM25 + original vector): weight = 2.0
  Additional lists (HyDE variants): weight = 1.0
  topRankBonus = +0.05 if best rank is #1
               = +0.02 if best rank is #2 or #3
               = 0 otherwise

Sorting uses raw RRF score. Final score exposed to users = 1/rank (range (0, 1]), referencing QMD.
```

Group by ChunkId, preserving multiple chunks from same document.

## 6. Search Command Semantics

Unified naming (CLI + MCP + HTTP API):

| Command | Meaning | Implementation |
|---------|---------|---------------|
| `search` | BM25 keyword search | FTS5 via chunks_fts |
| `vsearch` | Vector semantic search | Ollama embedding + chunks_vec |
| `query` | Hybrid search + HyDE | RRF fusion of BM25 + vector + optional HyDE expansion |

MCP tools exposed:
- `search` → Hybrid (BM25 + vector + optional HyDE) with RRF fusion
- `search_lex` → BM25 keyword search
- `search_vector` → vector semantic search

## 7. collection add Auto-Index

When `collection add` is called:
1. Register collection in DB (as now)
2. Immediately trigger index scan for the new collection
3. Output: `Collection 'x' added: /path (15 docs indexed)`

Combined with daemon's background indexer, files stay indexed automatically.

### Timestamp-based Change Detection

- Store `file_mod_time` (Unix nanoseconds) in `documents` table
- Index scan: compare os.Stat().ModTime() with stored value
- Only compute SHA-256 and re-index when timestamp changes
- Schema migration: `ALTER TABLE documents ADD COLUMN file_mod_time INTEGER DEFAULT 0`

## 8. HyDE Query Expansion (Phase 2)

Using Ollama + general-purpose model (方案 A):

When `query` is called:
1. Generate hypothetical document via Ollama: "Given query '{q}', write a short passage that would answer this query"
2. Embed the hypothetical document
3. Add it as an additional vector search list in RRF fusion (weight 1.0, not 2.0)
4. This improves recall for queries where the user's phrasing differs from the document's

Config addition:
```yaml
hyde:
  enabled: true
  model: qwen3:0.6b-q8_0  # or any Ollama model
```

## 9. Files Changed (Summary)

### New Files
- `internal/config/config.go` — config loading, defaults, save
- `internal/daemon/server.go` — HTTP API server
- `internal/daemon/routes.go` — route handlers
- `internal/daemon/daemon.go` — daemon lifecycle (start, stop, background goroutines)
- `internal/daemon/client.go` — CLI client that talks to daemon
- `internal/embedding/ollama.go` — Ollama-only provider

### Modified Files
- `internal/cli/root.go` — add config.Load(), daemon auto-start
- `internal/cli/*.go` — all commands become thin HTTP clients
- `internal/service/fusion.go` — replace weighted linear with RRF
- `internal/service/indexer.go` — add timestamp fast-path
- `internal/dao/schema.go` — add file_mod_time column
- `internal/dao/document.go` — store/compare file_mod_time
- `internal/cli/mcp.go` → removed (daemon handles MCP)
- `internal/mcp/` → adapted to work within daemon

### Deleted Files
- `internal/embedding/gguf.go` — replaced by ollama.go

## 10. Implementation Order

1. **Config system** — `internal/config/`, YAML loading, defaults
2. **Daemon core** — HTTP server, lifecycle, PID management
3. **CLI → Daemon migration** — commands become HTTP clients
4. **Background indexer** — poll goroutine, timestamp detection
5. **Ollama-only embedding** — remove llama-server, rename provider
6. **Background embedder** — continuous embedding goroutine
7. **Fusion → RRF** — replace algorithm + tests
8. **MCP in daemon** — MCP as daemon endpoint
9. **HyDE** — Ollama-based query expansion
10. **Collection add auto-index** — immediate indexing on add
11. **Memory layer** — memories table + memory_add/search + time decay

## 11. Agent Memory Layer

LMD doubles as a memory layer for AI agents. Agents store and retrieve memories via MCP tools or CLI commands.

### Memory Table

Independent from the document/collection system. Shares the embedding provider.

```sql
CREATE TABLE memories (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    content     TEXT NOT NULL,
    type        TEXT NOT NULL DEFAULT 'episode',  -- fact | episode | relation
    embedding   BLOB,                              -- float32 vector (via sqlite-vec or blob)
    created_at  DATETIME DEFAULT (DATETIME('now', '+8 hours'))
);

CREATE VIRTUAL TABLE memories_fts USING fts5(
    content,
    content='memories',
    content_rowid='id',
    tokenize='porter unicode61'
);
```

### Memory Types and Decay

| Type | Description | Half-life | Decay behavior |
|------|-------------|-----------|---------------|
| `fact` | Factual knowledge | Never | Score unchanged |
| `episode` | Events/experiences | 15 days | `score × 0.5^(age_days/15)` |
| `relation` | Preferences/associations | 180 days | `score × 0.5^(age_days/180)` |

### Operations

Only two operations — no explicit delete. Memories are never physically deleted; old memories naturally become irrelevant through score decay during search.

**memory_add**: Insert a memory. Content only — embedding done by background embedWorker.
- MCP tool: `memory_add(content: string, type: "fact"|"episode"|"relation")`
- CLI: `lmd memory add "..." --type episode`
- Returns: `{id, type, created_at}`

**memory_search**: Search memories with time-decay scoring.
- MCP tool: `memory_search(query: string, limit?: number, type?: string)`
- CLI: `lmd memory search "..." --type episode`
- Scoring: raw_score × decay_factor (by type and age)
- Returns: `[{id, content, type, score, created_at}]`

### Time Decay in Search

Applied at query time, not stored. For each search result:

```
age_days = (now - created_at).Hours() / 24
decay = type == "fact" ? 1.0 : 0.5^(age_days / half_life)
final_score = raw_score * decay
```

This means:
- A 15-day-old episode gets 50% of its original score
- A 30-day-old episode gets 25%
- A 180-day-old relation gets 50%
- Facts always get 100%

### Design Notes for Future-proofing

1. **Schema coexists** with documents — no conflicts, same SQLite DB
2. **Embedding shared** — memories use the same Ollama provider as documents
3. **MCP tools are additive** — `memory_add` and `memory_search` don't affect existing tools
4. **No delete needed** — decay handles relevance naturally; physical storage is cheap
5. **Collection filtering is separate** — `memory_search` queries memories table, `search`/`query` query documents

## 12. Testing

1. Unit: config load/save, default generation
2. Unit: OllamaProvider with mock HTTP server
3. Unit: RRF fusion algorithm (comprehensive test cases from QMD)
4. Unit: timestamp-based change detection
5. Integration: daemon start → CLI command → response
6. Integration: file change → auto-reindex → search finds new content
7. Integration: collection add → immediate indexing → correct doc count
8. Unit: memory_add + memory_search with time decay
9. Integration: MCP memory_add → memory_search finds it
