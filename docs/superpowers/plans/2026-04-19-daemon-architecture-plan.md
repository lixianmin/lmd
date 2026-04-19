# LMD Daemon Architecture Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform LMD from a per-command CLI tool into a client-server daemon architecture with config system, Ollama-only embedding, RRF fusion, timestamp-based indexing, HyDE query expansion, and agent memory layer.

**Architecture:** CLI is a thin HTTP client. Daemon is a persistent background process handling all indexing/embedding/search/MCP/memory. Communication via HTTP JSON on localhost.

**Tech Stack:** Go, SQLite (mattn/go-sqlite3 + sqlite-vec), cobra, gopkg.in/yaml.v3, net/http (stdlib)

**Spec:** `docs/superpowers/specs/2026-04-19-daemon-config-ollama-design.md`

---

## File Structure

### New Files
| File | Responsibility |
|------|---------------|
| `internal/config/config.go` | Config struct, Load(), SaveDefault(), DefaultConfig() |
| `internal/config/config_test.go` | Config unit tests |
| `internal/daemon/daemon.go` | Daemon lifecycle: Start(), Stop(), PID management |
| `internal/daemon/server.go` | HTTP API server, route registration |
| `internal/daemon/routes.go` | HTTP handler functions for each endpoint |
| `internal/daemon/client.go` | Client helper: daemon HTTP calls, auto-start, health check |
| `internal/daemon/client_test.go` | Client unit tests |
| `internal/embedding/ollama.go` | OllamaProvider (replaces GGUFProvider) |
| `internal/embedding/ollama_test.go` | Ollama provider tests |
| `internal/service/rrf.go` | RRF fusion algorithm |
| `internal/service/rrf_test.go` | RRF fusion tests |
| `internal/service/memory.go` | MemoryAdd, MemorySearch with time-decay |
| `internal/service/memory_test.go` | Memory service tests |
| `internal/dao/memory.go` | Memory table CRUD |
| `internal/dao/memory_test.go` | Memory DAO tests |
| `internal/service/hyde.go` | HyDE query expansion via Ollama |
| `internal/service/hyde_test.go` | HyDE tests |

### Modified Files
| File | Change |
|------|--------|
| `go.mod` | Add `gopkg.in/yaml.v3` |
| `internal/cli/root.go` | Add config.Load(), daemon auto-start |
| `internal/cli/collection.go` | Become HTTP client |
| `internal/cli/index.go` | Become HTTP client |
| `internal/cli/embed.go` | Become HTTP client |
| `internal/cli/search.go` | Become HTTP client |
| `internal/cli/get.go` | Become HTTP client |
| `internal/cli/mcp.go` | Remove (daemon handles MCP) |
| `internal/service/fusion.go` | Replace with RRF call |
| `internal/service/fusion_test.go` | Replace with RRF tests |
| `internal/service/indexer.go` | Add timestamp fast-path |
| `internal/dao/schema.go` | Add file_mod_time column, memories table |
| `internal/dao/document.go` | Add FileModTime to DocumentRecord |
| `internal/dao/chunks_fts.go` | No changes |
| `internal/service/embedder.go` | Read batch_size from config |
| `internal/mcp/server.go` | Adapt to run inside daemon |

### Deleted Files
| File | Reason |
|------|--------|
| `internal/embedding/gguf.go` | Replaced by ollama.go |

---

## Chunk 1: Config System

### Task 1: Add YAML dependency

**Files:**
- Modify: `go.mod`

- [ ] **Step 1: Add gopkg.in/yaml.v3**

```bash
cd /Users/xmli/me/code/lmd && go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Verify**

```bash
cd /Users/xmli/me/code/lmd && go build -tags fts5 ./...
```

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum && git commit -m "chore: add gopkg.in/yaml.v3 dependency"
```

### Task 2: Config package

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/config_test.go`

- [ ] **Step 1: Write config_test.go**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Daemon.Port != 18200 {
		t.Fatalf("expected port 18200, got %d", cfg.Daemon.Port)
	}
	if cfg.Embedding.Ollama.URL != "http://localhost:11434" {
		t.Fatalf("unexpected ollama url: %s", cfg.Embedding.Ollama.URL)
	}
	if cfg.Embedding.BatchSize != 8 {
		t.Fatalf("expected batch_size 8, got %d", cfg.Embedding.BatchSize)
	}
	if cfg.Vector.Dimensions != 1024 {
		t.Fatalf("expected dimensions 1024, got %d", cfg.Vector.Dimensions)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	origDir := configDir
	configDir = dir
	defer func() { configDir = origDir }()

	cfg := DefaultConfig()
	cfg.Daemon.Port = 19999

	if err := SaveDefault(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, "config.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Daemon.Port != 19999 {
		t.Fatalf("expected port 19999, got %d", loaded.Daemon.Port)
	}
}

func TestLoadMissingReturnsDefault(t *testing.T) {
	dir := t.TempDir()
	origDir := configDir
	configDir = dir
	defer func() { configDir = origDir }()

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Daemon.Port != 18200 {
		t.Fatalf("expected default port 18200, got %d", loaded.Daemon.Port)
	}
}
```

- [ ] **Step 2: Run tests — expect failure**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/config/ -v -run "TestDefault|TestSaveAnd|TestLoadMissing"
```

Expected: FAIL (package doesn't exist)

- [ ] **Step 3: Write config.go**

```go
package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Daemon    DaemonConfig    `yaml:"daemon"`
	Embedding EmbeddingConfig `yaml:"embedding"`
	Vector    VectorConfig    `yaml:"vector"`
	Database  DatabaseConfig  `yaml:"database"`
}

type DaemonConfig struct {
	Port             int    `yaml:"port"`
	IdleTimeout      string `yaml:"idle_timeout"`
	IndexPollInterval string `yaml:"index_poll_interval"`
}

type EmbeddingConfig struct {
	Provider  string       `yaml:"provider"`
	Ollama    OllamaConfig `yaml:"ollama"`
	BatchSize int          `yaml:"batch_size"`
	Truncation int         `yaml:"truncation"`
}

type OllamaConfig struct {
	URL       string `yaml:"url"`
	Model     string `yaml:"model"`
	KeepAlive string `yaml:"keep_alive"`
}

type VectorConfig struct {
	Dimensions     int    `yaml:"dimensions"`
	DistanceMetric string `yaml:"distance_metric"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

var Cfg *Config

var configDir string

func init() {
	home, _ := os.UserHomeDir()
	configDir = filepath.Join(home, ".config", "lmd")
}

func DefaultConfig() *Config {
	return &Config{
		Daemon: DaemonConfig{
			Port:              18200,
			IdleTimeout:       "30m",
			IndexPollInterval: "60s",
		},
		Embedding: EmbeddingConfig{
			Provider: "ollama",
			Ollama: OllamaConfig{
				URL:       "http://localhost:11434",
				Model:     "qwen3-embedding:0.6b-q8_0",
				KeepAlive: "30m",
			},
			BatchSize:  8,
			Truncation: 800,
		},
		Vector: VectorConfig{
			Dimensions:     1024,
			DistanceMetric: "cosine",
		},
		Database: DatabaseConfig{
			Path: filepath.Join(homeDir(), ".cache", "lmd", "index.sqlite"),
		},
	}
}

func Load() (*Config, error) {
	path := filepath.Join(configDir, "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultConfig()
			Cfg = cfg
			return cfg, nil
		}
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	Cfg = &cfg
	return &cfg, nil
}

func SaveDefault() error {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(configDir, "config.yaml")
	data, err := yaml.Marshal(DefaultConfig())
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/config/ -v
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/ && git commit -m "feat: config system with YAML loading and defaults"
```

---

## Chunk 2: Ollama-only Embedding Provider

### Task 3: Write OllamaProvider

**Files:**
- Create: `internal/embedding/ollama.go`
- Create: `internal/embedding/ollama_test.go`

- [ ] **Step 1: Write ollama_test.go**

Test with `httptest.NewServer` mock:

```go
package embedding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOllamaProvider_Embed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"embeddings": [][]float32{{0.1, 0.2, 0.3}},
			})
			return
		}
		if r.URL.Path == "/api/tags" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"models": []map[string]interface{}{{"name": "test-model"}},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, "test-model")
	vec, err := p.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(vec) != 3 {
		t.Fatalf("expected 3 dims, got %d", len(vec))
	}
}

func TestOllamaProvider_EmbedBatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"embeddings": [][]float32{{0.1, 0.2}, {0.3, 0.4}},
		})
	}))
	defer server.Close()

	p := NewOllamaProvider(server.URL, "test-model")
	vecs, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("expected 2 vectors, got %d", len(vecs))
	}
}

func TestOllamaProvider_Dimension(t *testing.T) {
	p := NewOllamaProvider("http://localhost:11434", "test")
	if p.Dimension() != 1024 {
		t.Fatalf("expected 1024, got %d", p.Dimension())
	}
}
```

- [ ] **Step 2: Run tests — expect failure**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/embedding/ -v -run "TestOllama"
```

- [ ] **Step 3: Write ollama.go**

Extract only the Ollama HTTP logic from `gguf.go`. Keep `callOllamaEmbed`, `ollamaAvailable`, `ollamaModelExists`, `ollamaPull`, `warmup`. Remove all llama-server code.

```go
package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/lixianmin/logo"
)

type OllamaProvider struct {
	baseURL string
	model   string
	client  *http.Client
}

func NewOllamaProvider(url, model string) *OllamaProvider {
	return &OllamaProvider{
		baseURL: url,
		model:   model,
		client:  &http.Client{Timeout: 120 * time.Second},
	}
}

func (my *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	vecs, err := my.EmbedBatch(ctx, []string{text})
	if err != nil {
		return nil, err
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("no embedding returned")
	}
	return vecs[0], nil
}

func (my *OllamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	payload := map[string]interface{}{
		"model": my.model,
		"input": texts,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", my.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := my.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ollama embed request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama embed returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Embeddings [][]float32 `json:"embeddings"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ollama embed decode failed: %w", err)
	}
	return result.Embeddings, nil
}

func (my *OllamaProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return my.Embed(ctx, query)
}

func (my *OllamaProvider) Dimension() int { return 1024 }

func (my *OllamaProvider) ModelName() string { return my.model }

func (my *OllamaProvider) Close() error { return nil }

func OllamaAvailable(url string) bool {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url + "/api/tags")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

func EnsureOllamaModel(url, model string) error {
	if OllamaAvailable(url) {
		return nil
	}
	return fmt.Errorf("ollama not available at %s", url)
}
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/embedding/ -v -run "TestOllama"
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/embedding/ollama.go internal/embedding/ollama_test.go && git commit -m "feat: OllamaProvider (Ollama-only, no llama-server)"
```

### Task 4: Migrate callers from GGUFProvider to OllamaProvider

**Files:**
- Modify: `internal/cli/search.go`
- Modify: `internal/cli/embed.go`
- Modify: `internal/service/embedder.go`
- Modify: `internal/service/searcher.go`
- Modify: `internal/mcp/server.go` (references)

- [ ] **Step 1: Update search.go** — Replace `newProvider()` to use `OllamaProvider`:

Change `func newProvider() *embedding.GGUFProvider` to:
```go
func newProvider() *embedding.OllamaProvider {
	embedding.EnsureOllamaModel(config.Cfg.Embedding.Ollama.URL, config.Cfg.Embedding.Ollama.Model)
	return embedding.NewOllamaProvider(config.Cfg.Embedding.Ollama.URL, config.Cfg.Embedding.Ollama.Model)
}
```

Update return type references in `vsearchCmd` and `queryCmd`.

- [ ] **Step 2: Update embed.go** — Same pattern, use `OllamaProvider`

- [ ] **Step 3: Update service/searcher.go** — Change `embedding.EmbeddingProvider` parameter types if needed (interface unchanged, only concrete type changes)

- [ ] **Step 4: Build and test**

```bash
cd /Users/xmli/me/code/lmd && go build -tags fts5 ./... && go test -tags fts5 ./...
```

- [ ] **Step 5: Delete gguf.go**

```bash
rm internal/embedding/gguf.go
```

- [ ] **Step 6: Build and test again**

```bash
cd /Users/xmli/me/code/lmd && go build -tags fts5 ./... && go test -tags fts5 ./...
```

- [ ] **Step 7: Commit**

```bash
git add -A && git commit -m "refactor: migrate from GGUFProvider to OllamaProvider, remove llama-server code"
```

---

## Chunk 3: Daemon Core

### Task 5: Daemon lifecycle

**Files:**
- Create: `internal/daemon/daemon.go`

- [ ] **Step 1: Write daemon.go**

Contains: `Start()`, `Stop()`, PID file management, signal handling, idle timeout.

Key functions:
- `Start(ctx context.Context, cfg *config.Config)` — starts HTTP server + background goroutines
- `Stop()` — graceful shutdown
- `writePid(path string)`, `readPid(path string) (int, error)`, `isAlive(pid int) bool`
- `DaemonPIDPath()` → `~/.cache/lmd/daemon.pid`

This is the main orchestration file. It:
1. Opens SQLite via `dao.Init()`
2. Starts HTTP server on `cfg.Daemon.Port`
3. Launches index poller goroutine (every `cfg.Daemon.IndexPollInterval`)
4. Launches embed worker goroutine
5. Handles SIGINT/SIGTERM
6. Tracks last activity for idle timeout

- [ ] **Step 2: Commit**

```bash
git add internal/daemon/daemon.go && git commit -m "feat: daemon lifecycle with PID management and idle timeout"
```

### Task 6: HTTP API server

**Files:**
- Create: `internal/daemon/server.go`
- Create: `internal/daemon/routes.go`

- [ ] **Step 1: Write server.go**

`NewServer(port int) *http.Server` with mux registration. Graceful shutdown via `Shutdown(ctx)`.

- [ ] **Step 2: Write routes.go**

Handler functions for each endpoint in the spec:

| Function | Route |
|----------|-------|
| `handleHealth` | GET /health |
| `handleSearch` | POST /search |
| `handleVsearch` | POST /vsearch |
| `handleQuery` | POST /query |
| `handleGet` | POST /get |
| `handleStatus` | GET /status |
| `handleCollectionAdd` | POST /collection/add |
| `handleCollectionRemove` | POST /collection/remove |
| `handleCollectionList` | GET /collection/list |
| `handleCollectionRename` | POST /collection/rename |
| `handleUpdate` | POST /update |
| `handleEmbed` | POST /embed |
| `handleRebuild` | POST /rebuild |
| `handleMemoryAdd` | POST /memory/add |
| `handleMemorySearch` | POST /memory/search |

Each handler:
1. Decode JSON request
2. Call service/dao layer
3. Encode JSON response

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/server.go internal/daemon/routes.go && git commit -m "feat: daemon HTTP API server with all endpoints"
```

### Task 7: Daemon client (CLI side)

**Files:**
- Create: `internal/daemon/client.go`
- Create: `internal/daemon/client_test.go`

- [ ] **Step 1: Write client.go**

```go
package daemon

type Client struct {
	baseURL string
	client  *http.Client
}

func NewClient(port int) *Client

func (c *Client) IsAlive() bool                    // GET /health
func (c *Client) StartDaemon() error               // exec.Command("lmd", "daemon", "--detach")
func (c *Client) EnsureDaemon() error              // IsAlive() or StartDaemon() + wait
func (c *Client) Search(req SearchRequest) ([]byte, error)  // POST /search
func (c *Client) VSearch(req VSearchRequest) ([]byte, error)
func (c *Client) Query(req QueryRequest) ([]byte, error)
func (c *Client) Get(req GetRequest) ([]byte, error)
func (c *Client) Status() ([]byte, error)
func (c *Client) CollectionAdd(req ColAddRequest) ([]byte, error)
func (c *Client) CollectionRemove(name string) ([]byte, error)
func (c *Client) CollectionList() ([]byte, error)
func (c *Client) CollectionRename(old, new string) ([]byte, error)
func (c *Client) Update(collection string) ([]byte, error)
func (c *Client) Embed() ([]byte, error)
func (c *Client) Rebuild() ([]byte, error)
func (c *Client) MemoryAdd(req MemoryAddRequest) ([]byte, error)
func (c *Client) MemorySearch(req MemorySearchRequest) ([]byte, error)
```

`EnsureDaemon()`:
1. GET /health
2. If OK → return
3. If not → exec.Command("lmd", "daemon", "--detach")
4. Poll GET /health every 100ms, timeout 30s

- [ ] **Step 2: Write client_test.go** — Test with httptest mock server

- [ ] **Step 3: Run tests**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/daemon/ -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/client.go internal/daemon/client_test.go && git commit -m "feat: daemon HTTP client with auto-start logic"
```

---

## Chunk 4: CLI → Daemon Migration

### Task 8: Add daemon command to CLI

**Files:**
- Create: `internal/cli/daemon.go`

- [ ] **Step 1: Write daemon.go**

`lmd daemon [--detach]` command:
- `--detach`: fork to background using `exec.Command(os.Args[0], "daemon")` with `SysProcAttr.Setpgid=true`, return immediately
- Without `--detach`: call `daemon.Start(ctx, config.Cfg)` in foreground

- [ ] **Step 2: Commit**

```bash
git add internal/cli/daemon.go && git commit -m "feat: lmd daemon command with --detach flag"
```

### Task 9: Migrate CLI commands to HTTP clients

**Files:**
- Modify: `internal/cli/root.go` — Add config.Load(), daemon.EnsureDaemon()
- Modify: `internal/cli/collection.go` — HTTP client calls
- Modify: `internal/cli/index.go` — HTTP client calls
- Modify: `internal/cli/embed.go` — HTTP client calls
- Modify: `internal/cli/search.go` — HTTP client calls
- Modify: `internal/cli/get.go` — HTTP client calls
- Remove: `internal/cli/mcp.go`

This is the largest refactor. Each command's `RunE` changes from:
```go
// Before: direct service call
tok, _ := tokenizer.NewGseTokenizer()
searcher := service.NewSearcher(tok)
results, _ := searcher.SearchLex(query, collection, limit, minScore)
```
To:
```go
// After: HTTP client call
client := daemon.NewClient(config.Cfg.Daemon.Port)
body, _ := client.Search(daemon.SearchRequest{Query: query, Collection: collection, Limit: limit, MinScore: minScore})
fmt.Print(string(body))
```

**Strategy**: One command at a time, test after each. Start with simplest (status, collection list), work up to most complex (search, query).

- [ ] **Step 1: Update root.go** — Add config.Load() and client initialization

- [ ] **Step 2: Migrate status command** — simplest, good starting point

- [ ] **Step 3: Migrate collection commands** — add/remove/list/rename

- [ ] **Step 4: Migrate search/vsearch/query** — the complex ones

- [ ] **Step 5: Migrate get command**

- [ ] **Step 6: Migrate update/embed/rebuild**

- [ ] **Step 7: Remove mcp.go** — MCP is now a daemon endpoint

- [ ] **Step 8: Build and run full test suite**

```bash
cd /Users/xmli/me/code/lmd && go build -tags fts5 ./... && go test -tags fts5 ./...
```

- [ ] **Step 9: Commit**

```bash
git add -A && git commit -m "refactor: CLI commands become HTTP clients to daemon"
```

---

## Chunk 5: Timestamp-based Indexing + Auto-index on collection add

### Task 10: Schema migration — add file_mod_time

**Files:**
- Modify: `internal/dao/schema.go`
- Modify: `internal/dao/document.go`
- Modify: `internal/service/indexer.go`

- [ ] **Step 1: Update schema.go** — Add `ALTER TABLE documents ADD COLUMN file_mod_time INTEGER DEFAULT 0` in `createTables()` (idempotent, SQLite ignores error if exists)

- [ ] **Step 2: Update document.go** — Add `FileModTime int64` to `DocumentRecord`. Update `UpsertDocument()` to store mod time. Update `ListDocumentsByCollection()` to return `file_mod_time`.

- [ ] **Step 3: Update indexer.go** — Two-pass:
  1. `os.Stat(path)` → get mod time
  2. Compare with stored `FileModTime`
  3. If unchanged → skip (increment Unchanged)
  4. If changed → read file, compute hash, index as before, store new mod time

- [ ] **Step 4: Update collection add** — After `dao.AddCollection()`, immediately call `idx.UpdateCollection()` and print doc count.

- [ ] **Step 5: Run tests**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./... -v
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: timestamp-based change detection + auto-index on collection add"
```

---

## Chunk 6: Background Indexer + Embedder

### Task 11: Background goroutines in daemon

**Files:**
- Modify: `internal/daemon/daemon.go`

- [ ] **Step 1: Add index poller goroutine** — Every 60s (from config), scan all collections using the timestamp-based indexer.

- [ ] **Step 2: Add embed worker goroutine** — Continuously embed unembedded chunks in batches.

- [ ] **Step 3: Test manually** — Start daemon, add files, watch them get indexed and embedded.

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "feat: background index poller and embed worker goroutines"
```

---

## Chunk 7: RRF Fusion

### Task 12: Implement RRF algorithm

**Files:**
- Create: `internal/service/rrf.go`
- Create: `internal/service/rrf_test.go`
- Modify: `internal/service/fusion.go`
- Modify: `internal/service/fusion_test.go`

- [ ] **Step 1: Write rrf_test.go**

Comprehensive tests matching QMD behavior:
- Basic RRF with 2 lists
- Weighted lists (first 2 get 2x)
- Top-rank bonus
- Multiple chunks from same doc preserved
- Empty lists
- Single list

- [ ] **Step 2: Run tests — expect failure**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/service/ -v -run "TestRRF"
```

- [ ] **Step 3: Write rrf.go**

```go
package service

type RRFParams struct {
	K             int
	Weights       []float64
	TopRankBonus1 float64
	TopRankBonus2 float64
}

func DefaultRRFParams() RRFParams {
	return RRFParams{
		K:             60,
		TopRankBonus1: 0.05,
		TopRankBonus2: 0.02,
	}
}

func ReciprocalRankFusion(lists [][]int64, params RRFParams) []RRFResult {
	// For each chunk across all lists:
	//   rrfScore = SUM(weight_i / (k + rank_i + 1))
	//   topRankBonus = 0.05 if best rank == 0, 0.02 if best rank <= 2
	// Sort by rrfScore descending
}

type RRFResult struct {
	ChunkId int64
	Score   float64
}
```

- [ ] **Step 4: Update fusion.go** — Replace `FuseResults()` to use RRF internally. Keep the same function signature for compatibility.

- [ ] **Step 5: Update fusion_test.go** — Replace all tests with RRF-based expected values.

- [ ] **Step 6: Run all tests**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/service/ -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/service/rrf.go internal/service/rrf_test.go internal/service/fusion.go internal/service/fusion_test.go && git commit -m "feat: RRF fusion replacing weighted linear combination"
```

---

## Chunk 8: Memory Layer

### Task 13: Memory DAO

**Files:**
- Create: `internal/dao/memory.go`
- Create: `internal/dao/memory_test.go`
- Modify: `internal/dao/schema.go`

- [ ] **Step 1: Update schema.go** — Add `memories` table and `memories_fts` virtual table to `createTables()`.

- [ ] **Step 2: Write memory_test.go**

Tests for: InsertMemory, SearchMemoryFTS, GetMemoriesByIds

- [ ] **Step 3: Write memory.go**

```go
type MemoryRecord struct {
	Id        int64
	Content   string
	Type      string
	CreatedAt time.Time
}

func InsertMemory(content, memType string) (int64, error)
func SearchMemoryFTS(tokenizedQuery string, limit int) ([]MemoryRecord, error)
func GetMemoriesByIds(ids []int64) ([]MemoryRecord, error)
```

- [ ] **Step 4: Run tests**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/dao/ -v -run "TestMemory"
```

- [ ] **Step 5: Commit**

```bash
git add internal/dao/memory.go internal/dao/memory_test.go internal/dao/schema.go && git commit -m "feat: memory table DAO with FTS search"
```

### Task 14: Memory service with time decay

**Files:**
- Create: `internal/service/memory.go`
- Create: `internal/service/memory_test.go`

- [ ] **Step 1: Write memory_test.go**

Test cases:
- Add fact, search finds it with no decay
- Add episode, search finds it; mock time to 15 days later, score should be 50%
- Add relation, 180 days later, score should be 50%
- Filter by type

- [ ] **Step 2: Write memory.go**

```go
type MemoryService struct {
	tokenizer tokenizer.Tokenizer
	provider  embedding.EmbeddingProvider
}

func (my *MemoryService) Add(content, memType string) (int64, error)
func (my *MemoryService) Search(query string, limit int, memType string) ([]MemorySearchResult, error)
```

Time decay logic:
```go
func decayFactor(memType string, ageDays float64) float64 {
	switch memType {
	case "fact":
		return 1.0
	case "episode":
		return math.Pow(0.5, ageDays/15.0)
	case "relation":
		return math.Pow(0.5, ageDays/180.0)
	default:
		return 1.0
	}
}
```

- [ ] **Step 3: Add daemon routes** — Wire `handleMemoryAdd` and `handleMemorySearch` to MemoryService

- [ ] **Step 4: Add CLI commands** — `lmd memory add "..." --type episode`, `lmd memory search "..." --type episode`

- [ ] **Step 5: Run tests**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/service/ -v -run "TestMemory"
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: memory service with time-decay scoring, CLI and API endpoints"
```

---

## Chunk 9: HyDE Query Expansion

### Task 15: HyDE via Ollama

**Files:**
- Create: `internal/service/hyde.go`
- Create: `internal/service/hyde_test.go`
- Modify: `internal/daemon/routes.go` (query handler)
- Modify: `internal/config/config.go` (add hyde config)

- [ ] **Step 1: Add hyde config to config.go**

```yaml
hyde:
  enabled: true
  model: qwen3:0.6b-q8_0
```

- [ ] **Step 2: Write hyde_test.go**

Test with mock HTTP server returning a hypothetical document.

- [ ] **Step 3: Write hyde.go**

```go
func GenerateHypotheticalDocument(ctx context.Context, ollamaURL, model, query string) (string, error)
```

Sends to Ollama chat API:
```
Given the following search query, write a short passage that would answer this query. Keep it under 200 words.

Query: {query}
```

- [ ] **Step 4: Wire into query handler** — When HyDE enabled, generate hypothetical doc, embed it, add as third RRF list (weight 1.0)

- [ ] **Step 5: Run tests**

```bash
cd /Users/xmli/me/code/lmd && go test -tags fts5 ./internal/service/ -v -run "TestHyDE"
```

- [ ] **Step 6: Commit**

```bash
git add -A && git commit -m "feat: HyDE query expansion via Ollama general model"
```

---

## Chunk 10: MCP in Daemon + Integration Tests

### Task 16: MCP server inside daemon

**Files:**
- Modify: `internal/daemon/daemon.go` — Add MCP endpoint
- Modify: `internal/mcp/server.go` — Adapt to work with HTTP transport

- [ ] **Step 1: Add MCP HTTP handler** — Route `/mcp` to MCP protocol handler

- [ ] **Step 2: Update MCP tools** — Align with daemon spec:
  - `search` → BM25
  - `search_vector` → vector
  - `query` → hybrid + HyDE
  - `memory_add` → add memory
  - `memory_search` → search memories
  - `get`, `status`, `list_collections` → unchanged

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "feat: MCP server integrated into daemon with updated tools"
```

### Task 17: Update integration tests

**Files:**
- Modify: `tests/test_basic.sh`
- Modify: `tests/test_vector.sh`
- Create: `tests/test_memory.sh`

- [ ] **Step 1: Update test_basic.sh** — Tests now go through daemon. Add daemon startup at beginning, shutdown at end.

- [ ] **Step 2: Update test_vector.sh** — Same pattern.

- [ ] **Step 3: Write test_memory.sh** — Test memory add/search with type filtering.

- [ ] **Step 4: Run all integration tests**

```bash
cd /Users/xmli/me/code/lmd && make integration-basic && make integration-vector && bash tests/test_memory.sh
```

- [ ] **Step 5: Commit**

```bash
git add tests/ && git commit -m "test: update integration tests for daemon architecture, add memory tests"
```

---

## Chunk 11: Final Cleanup

### Task 18: Update memory.md and documentation

**Files:**
- Modify: `docs/01.memory.md`
- Modify: `Makefile`

- [ ] **Step 1: Update memory.md** — Reflect final daemon architecture

- [ ] **Step 2: Update Makefile** — Add daemon-related targets

- [ ] **Step 3: Final build + test**

```bash
cd /Users/xmli/me/code/lmd && go build -tags fts5 ./... && go test -tags fts5 ./... && go vet -tags fts5 ./...
```

- [ ] **Step 4: Commit**

```bash
git add -A && git commit -m "docs: update memory.md and Makefile for daemon architecture"
```
