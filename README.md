**[中文文档](README.zh.md)**

# LMD - Local Markdown Docs

A local hybrid search engine for Markdown documents with first-class Chinese language support. Written in Go.

LMD combines **BM25 keyword search** (via FTS5 + gse segmentation) with **vector semantic search** (via sqlite-vec) to provide fast, accurate search across your Markdown knowledge base. It works both as a CLI tool and an importable Go library.

## Features

- **Hybrid search**: BM25 keyword search + vector semantic search, with RRF fusion planned
- **Chinese-first**: gse tokenizer provides accurate Chinese word segmentation
- **Markdown-aware**: Chunks respect heading and code block boundaries
- **Single binary**: Compile and run, no external services needed
- **Go library**: Import `pkg/` for programmatic access
- **Agent-ready**: MCP server + JSON output planned for AI agent integration

## Install

```bash
go install -tags "fts5" github.com/lixianmin/lmd/cmd/lmd@latest
```

Or build from source:

```bash
git clone https://github.com/lixianmin/lmd.git
cd lmd
make install
```

> **Note:** The `fts5` build tag is required for full-text search support. CGo and a C compiler (GCC/Clang) are required for SQLite FTS5 and sqlite-vec.

## Quick Start

```bash
# Add a collection (directory of Markdown files)
lmd collection add ~/notes --name mynotes

# Index all collections (scan for new/changed/deleted files)
lmd update

# BM25 keyword search
lmd search "并发编程"
lmd search "goroutine channel" -n 10

# Vector semantic search
lmd embed
lmd vsearch "concurrent programming patterns"

# View document
lmd get mynotes/go.md
lmd get "#abc123"

# Check status
lmd status
```

## CLI Reference

| Command | Description |
|---------|-------------|
| `collection add <path>` | Add a document collection |
| `collection list` | List all collections |
| `collection remove <name>` | Remove a collection |
| `collection rename <old> <new>` | Rename a collection |
| `update` | Scan filesystem and update index |
| `embed` | Generate vector embeddings for chunks |
| `search <query>` | BM25 keyword search |
| `vsearch <query>` | Vector semantic search |
| `get <collection/path>` or `get <#docid>` | Retrieve a document |
| `status` | Show index status |

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--index` | `~/.cache/lmd/index.sqlite` | Database file path |
| `--collection, -c` | all | Limit to specific collection |
| `--limit, -n` | 5 | Number of results |
| `--full` | false | Show full document content |
| `--min-score` | 0 | Minimum score threshold |

## Go Library

```go
package main

import (
    "context"
    "fmt"

    "github.com/lixianmin/lmd/pkg/lmd"
)

func main() {
    store, err := lmd.CreateStore(lmd.StoreOptions{
        DBPath: "myindex.sqlite",
    })
    if err != nil {
        panic(err)
    }
    defer store.Close()

    store.AddCollection("notes", lmd.CollectionConfig{
        Path:        "/path/to/notes",
        GlobPattern: "**/*.md",
    })

    store.Update(context.Background(), lmd.UpdateOptions{})

    results, _ := store.SearchLex("并发编程", lmd.LexOptions{
        Limit: 5,
    })
    for _, r := range results {
        fmt.Printf("%s: %s (%.0f%%)\n", r.Path, r.Title, r.Score*100)
    }
}
```

## Architecture

```
cmd/lmd/              CLI entry point
internal/cli/         Cobra command definitions
internal/service/     Business logic (indexer, searcher, embedder)
internal/store/       SQLite persistence (FTS5 + sqlite-vec)
internal/tokenizer/   Text segmentation (gse)
internal/embedding/   Vector embedding abstraction
internal/chunker/     Markdown-aware document chunking
pkg/                  Public Go API
test/fixtures/        Test documents (Chinese + English)
```

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

## Tech Stack

| Component | Library | Purpose |
|-----------|---------|---------|
| CLI | [cobra](https://github.com/spf13/cobra) | Command framework |
| SQLite | [go-sqlite3](https://github.com/mattn/go-sqlite3) | Database (WAL mode) |
| Full-text search | FTS5 + [gse](https://github.com/go-ego/gse) | BM25 with Chinese tokenization |
| Vector search | [sqlite-vec](https://github.com/asg017/sqlite-vec) | KNN vector similarity |
| Embedding | Mock (Qwen3 planned) | Vector embedding generation |

## License

MIT

---

[中文文档](README.zh.md)
