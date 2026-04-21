**[中文文档](README.zh.md)**

# LMD - Local Markdown Docs

A local hybrid search engine for Markdown documents with first-class Chinese language support. Written in Go.

LMD combines **BM25 keyword search** (via FTS5 + gse segmentation) with **vector semantic search** (via sqlite-vec + Qwen3-Embedding), fused with RRF and re-ranked with MMR for diversity. It runs as a background daemon with CLI and MCP interfaces.

## Features

- **Hybrid search**: BM25 + vector search with RRF fusion, Rocchio PRF query expansion, MMR diversity re-ranking
- **HyDE search**: Hypothetical Document Embedding via SiliconFlow API for improved recall
- **Chinese-first**: gse tokenizer provides accurate Chinese word segmentation
- **Markdown-aware**: Chunks respect heading and code block boundaries (300 rune target)
- **Agent-ready**: MCP server + JSON output for AI agent integration
- **Agent memory**: Fact/episode/relation memory with time-decay scoring

## Install

```bash
git clone https://github.com/lixianmin/lmd.git
cd lmd
make install
```

> **Note:** CGo and a C compiler (GCC/Clang) are required for SQLite FTS5, sqlite-vec, and llama-go (embedding model). The llama-go submodule must be built first: `make submodule`.

## Quick Start

```bash
# Add a collection (directory of Markdown files)
lmd collection add ~/notes --name mynotes

# Search (daemon auto-starts, auto-indexes, auto-embeds)
lmd search "并发编程"
lmd vsearch "concurrent programming patterns"
lmd query "goroutine channel" -n 10

# HyDE search (requires hyde.api_key in config)
lmd hyde "how does Go handle concurrency"

# View document
lmd get mynotes/go.md
lmd get "#abc123"

# Agent memory
lmd memory add "Go uses goroutines for lightweight concurrency" --type fact
lmd memory search "concurrency"

# Daemon management
lmd status
lmd stop
lmd rebuild
```

## CLI Reference

| Command | Description |
|---------|-------------|
| `collection add <path> --name <n>` | Add a document collection |
| `collection list` | List all collections |
| `collection remove <name>` | Remove a collection |
| `collection rename <old> <new>` | Rename a collection |
| `search <query>` | BM25 keyword search |
| `vsearch <query>` | Vector semantic search |
| `query <query>` | Hybrid search (BM25 + vector + RRF fusion + MMR) |
| `hyde <query>` | HyDE search (vector search via hypothetical document) |
| `get <collection/path>` or `get <#docid>` | Retrieve a document |
| `memory add <content> --type <t>` | Add a memory (fact\|episode\|relation) |
| `memory search <query>` | Search memories |
| `status` | Show index status |
| `rebuild` | Drop all data and rebuild from scratch |
| `stop` | Stop the running daemon |

### Common Flags (search commands)

| Flag | Default | Description |
|------|---------|-------------|
| `--collection, -c` | all | Limit to specific collection |
| `--limit, -n` | 5 | Number of results |
| `--full` | false | Show full document content |
| `--min-score` | 0 | Minimum score threshold |
| `--format` | text | Output format (text\|md\|csv) |
| `--json` | false | JSON output (global flag) |
| `--verbose` | false | Verbose logging (global flag) |

## Architecture

```
cmd/lmd/              CLI entry point
internal/cli/         Cobra command definitions
internal/daemon/      HTTP daemon + background indexer/embedder
internal/service/     Business logic (indexer, searcher, embedder, memory)
internal/dao/         SQLite persistence (FTS5 + sqlite-vec)
internal/embedding/   Vector embedding (llama-go CGo, Qwen3-Embedding-0.6B)
internal/tokenizer/   Text segmentation (gse)
internal/chunker/     Markdown-aware chunking (300 rune, sentence-boundary overlap)
internal/formatter/   Output formatting (text/json/md/csv)
internal/config/      Config loading (YAML)
internal/mcp/         MCP protocol handler
test/fixtures/        Test documents (Chinese + English)
```

## Configuration

Config file: `~/.config/lmd/config.yaml`

```yaml
daemon:
  port: 12345

llama:
  embed_model: ~/.cache/lmd/models/Qwen3-Embedding-0.6B-Q8_0.gguf
  gpu_layers: -1
  threads: 4
  parallel: 8
  model_idle_timeout: 10m

embedding:
  batch_size: 8
  truncation: 300

hyde:
  base_url: https://api.siliconflow.cn/v1
  api_key: ""
  model: Qwen/Qwen3.5-9B
  max_tokens: 200

database:
  path: ~/.cache/lmd/index.sqlite
```

## Tech Stack

| Component | Library | Purpose |
|-----------|---------|---------|
| CLI | [cobra](https://github.com/spf13/cobra) | Command framework |
| SQLite | [go-sqlite3](https://github.com/mattn/go-sqlite3) | Database (WAL mode) |
| Full-text search | FTS5 + [gse](https://github.com/go-ego/gse) | BM25 with Chinese tokenization |
| Vector search | [sqlite-vec](https://github.com/asg017/sqlite-vec) | KNN cosine similarity (1024 dim) |
| Embedding | [llama-go](https://github.com/tcpipuk/llama-go) + Qwen3-Embedding-0.6B | Local vector embedding (Metal GPU) |
| HyDE | SiliconFlow API (OpenAI-compatible) | Hypothetical document generation |

## Development

```bash
make build          # Build binary
make test           # Run tests
make test-verbose   # Run tests with output
make vet            # Static analysis
make lint           # vet + fmt
make e2e            # End-to-end test
make all            # lint + test + build
make clean          # Remove built binary
```

## License

MIT

---

[中文文档](README.zh.md)
