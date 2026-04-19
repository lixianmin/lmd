# Post-Review Fix Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all correctness, safety, and design issues found in the code review of chunks 8-11.

**Architecture:** Targeted fixes to existing files. No new files. Each task is independent and can be committed separately.

**Tech Stack:** Go, SQLite (mattn/go-sqlite3 + sqlite-vec), cobra

**Build:** `go build -tags fts5 -mod=mod ./...`
**Test:** `go test -tags fts5 -mod=mod ./...`

---

## File Structure

### Modified Files
| File | Change |
|------|--------|
| `internal/daemon/routes.go` | Refactor handleQuery HyDE logic, register MCP tool handler |
| `internal/daemon/daemon.go` | Add MCP RegisterHandler call in Start() |
| `internal/dao/memory.go` | Use transaction in InsertMemory, validate memType |
| `internal/service/memory.go` | Validate memType in Add() |
| `internal/config/config.go` | Merge defaults in Load() |
| `internal/daemon/daemon.go` | Fix embedWorker backoff |
| `internal/service/fusion.go` | Clean up FuseResults signature |
| `internal/cli/root.go` | Remove unused --index flag |
| `internal/service/searcher.go` | Remove minScore from SearchVectorByEmbedding (already done) |

---

## Chunk 1: High Priority — Correctness

### Task 1: Refactor handleQuery to eliminate duplicate search

**Problem:** handleQuery calls SearchHybrid (FTS+vector+RRF) first, then if HyDE is enabled, calls SearchLex+SearchVector again plus a HyDE vector, doing FuseResultsThree. The first search is wasted when HyDE is on. Also, the condition `len(results) > 0` skips HyDE when it's most valuable (no results found).

**Files:**
- Modify: `internal/daemon/routes.go` (handleQuery function, ~lines 83-137)

- [ ] **Step 1: Rewrite handleQuery**

Replace the entire handleQuery function body with:

```go
func (my *Daemon) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 5
	}

	lexHits, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit*3, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	vecHits, err := my.searcher.SearchVector(my.provider, req.Query, req.Collection, req.Limit*3, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var results []formatter.SearchHit
	if my.cfg.HyDE.Enabled {
		hydeDoc, hydeErr := service.GenerateHypotheticalDocument(
			context.Background(),
			my.cfg.Embedding.Ollama.URL,
			my.cfg.HyDE.Model,
			req.Query,
		)
		if hydeErr == nil && hydeDoc != "" {
			hydeVec, embedErr := my.provider.EmbedQuery(context.Background(), hydeDoc)
			if embedErr == nil {
				hydeHits := my.searcher.SearchVectorByEmbedding(hydeVec, req.Collection, req.Limit*3)
				results = service.FuseResultsThree(lexHits, vecHits, hydeHits)
			}
		}
	}

	if len(results) == 0 {
		results = service.FuseResults(lexHits, vecHits)
	}

	if req.MinScore > 0 {
		var filtered []formatter.SearchHit
		for _, h := range results {
			if h.Score >= req.MinScore {
				filtered = append(filtered, h)
			}
		}
		results = filtered
	}

	if req.Limit > 0 && len(results) > req.Limit {
		results = results[:req.Limit]
	}

	logo.Info("handleQuery: query=%q collection=%s results=%d hyde=%v", req.Query, req.Collection, len(results), my.cfg.HyDE.Enabled)
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results})
}
```

Key changes:
- Always fetch lexHits and vecHits once
- If HyDE enabled: try HyDE → FuseResultsThree. If HyDE fails or empty: fall through to FuseResults
- If HyDE disabled: FuseResults (2-list RRF)
- Removed the `len(results) > 0` guard — HyDE always attempted when enabled

- [ ] **Step 2: Build and test**

```bash
go build -tags fts5 -mod=mod ./... && go test -tags fts5 -mod=mod ./...
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/routes.go && git commit -m "fix: refactor handleQuery to avoid duplicate search when HyDE enabled"
```

---

### Task 2: Register MCP tool handler in daemon

**Problem:** `handleMCP` calls `mcp.HandleRequest` which uses `mcp.toolHandler` global, but the daemon never calls `mcp.RegisterHandler()`. So `tools/call` always returns "no tool handler registered".

**Files:**
- Modify: `internal/daemon/daemon.go` (Start method, after registerRoutes)
- Modify: `internal/daemon/routes.go` (add new handleToolCall method)

- [ ] **Step 1: Add MCP handler registration in daemon.go Start()**

After `handler := registerRoutes(my)` (~line 71), add:

```go
	mcp.RegisterHandler(my.handleToolCall)
```

Add import `"github.com/lixianmin/lmd/internal/mcp"` to daemon.go.

- [ ] **Step 2: Add handleToolCall method to routes.go**

Add this method to routes.go. It dispatches MCP tool calls to the appropriate daemon handler by making internal HTTP calls (reusing the same handler logic):

```go
func (my *Daemon) handleToolCall(toolName string, params json.RawMessage) (interface{}, error) {
	switch toolName {
	case "search", "search_lex":
		var req struct {
			Query      string `json:"query"`
			Collection string `json:"collection"`
			Limit      int    `json:"limit"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = 5
		}
		hits, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit, 0)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"hits": hits}, nil

	case "search_vector":
		var req struct {
			Query      string  `json:"query"`
			Collection string  `json:"collection"`
			Limit      int     `json:"limit"`
			MinScore   float64 `json:"min_score"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = 5
		}
		if req.MinScore == 0 {
			req.MinScore = 0.3
		}
		hits, err := my.searcher.SearchVector(my.provider, req.Query, req.Collection, req.Limit, req.MinScore)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"hits": hits}, nil

	case "get":
		var req struct {
			Path string `json:"path"`
			Full bool   `json:"full"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		parts := strings.SplitN(req.Path, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("use collection/path format")
		}
		doc, err := dao.GetDocumentByPath(parts[0], parts[1])
		if err != nil {
			return nil, err
		}
		body := doc.Body
		if !req.Full && len(body) > 500 {
			body = body[:500] + "..."
		}
		return map[string]interface{}{
			"doc_id": dao.ShortDocId(doc.DocId), "title": doc.Title,
			"collection": doc.Collection, "path": doc.Path, "body": body,
		}, nil

	case "status":
		return my.buildStatus()

	case "list_collections":
		cols, err := dao.ListCollections()
		if err != nil {
			return nil, err
		}
		type colInfo struct {
			Name    string `json:"name"`
			Path    string `json:"path"`
			Glob    string `json:"glob"`
			DocCount int   `json:"doc_count"`
		}
		result := make([]colInfo, len(cols))
		for i, c := range cols {
			result[i] = colInfo{Name: c.Name, Path: c.Path, Glob: c.GlobPattern, DocCount: c.DocCount}
		}
		return result, nil

	case "memory_add":
		var req struct {
			Content string `json:"content"`
			Type    string `json:"type"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Content == "" {
			return nil, fmt.Errorf("content is required")
		}
		if req.Type == "" {
			req.Type = "episode"
		}
		id, err := my.memSvc.Add(req.Content, req.Type)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"id": id, "type": req.Type}, nil

	case "memory_search":
		var req struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
			Type  string `json:"type"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = 10
		}
		return my.memSvc.Search(req.Query, req.Limit, req.Type)

	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}
```

Also extract status building into a helper:

```go
func (my *Daemon) buildStatus() (interface{}, error) {
	cols, err := dao.ListCollections()
	if err != nil {
		return nil, err
	}
	totalDocs := 0
	type colStat struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Glob    string `json:"glob"`
		DocCount int   `json:"doc_count"`
	}
	stats := make([]colStat, len(cols))
	for i, c := range cols {
		stats[i] = colStat{Name: c.Name, Path: c.Path, Glob: c.GlobPattern, DocCount: c.DocCount}
		totalDocs += c.DocCount
	}
	chunkCount, embedCount := dao.GetChunkCounts()
	return map[string]interface{}{
		"documents": totalDocs, "chunks": chunkCount,
		"embedded": embedCount, "pending": chunkCount - embedCount,
		"collections": stats,
	}, nil
}
```

- [ ] **Step 3: Build and test**

```bash
go build -tags fts5 -mod=mod ./... && go test -tags fts5 -mod=mod ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/daemon/daemon.go internal/daemon/routes.go && git commit -m "fix: register MCP tool handler so tools/call works via /mcp endpoint"
```

---

### Task 3: Use transaction in InsertMemory

**Problem:** If FTS insert fails, the main record exists but is unsearchable. No error returned.

**Files:**
- Modify: `internal/dao/memory.go` (InsertMemory function)

- [ ] **Step 1: Rewrite InsertMemory with transaction**

```go
func InsertMemory(content, memType string) (int64, error) {
	var id int64
	err := withTransaction(func(tx *sql.Tx) error {
		res, err := tx.Exec("INSERT INTO memories (content, type) VALUES (?, ?)", content, memType)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		_, err = tx.Exec("INSERT INTO memories_fts (rowid, content) VALUES (?, ?)", id, content)
		return err
	})
	return id, err
}
```

Add `"database/sql"` to imports.

- [ ] **Step 2: Run tests**

```bash
go test -tags fts5 -mod=mod ./internal/dao/ -v -run "TestMemory|TestInsert|TestGet|TestSearch|TestUpdate"
```

Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/dao/memory.go && git commit -m "fix: wrap InsertMemory in transaction to ensure FTS consistency"
```

---

## Chunk 2: Medium Priority — Robustness

### Task 4: Validate memType in MemoryService.Add

**Problem:** No validation of memType — any string accepted. Unknown types get fact-like behavior (no decay).

**Files:**
- Modify: `internal/service/memory.go` (Add method)

- [ ] **Step 1: Add validation**

At the top of `Add()`, before `dao.InsertMemory`:

```go
func (my *MemoryService) Add(content, memType string) (int64, error) {
	switch memType {
	case "fact", "episode", "relation":
	default:
		return 0, fmt.Errorf("invalid memory type: %q (must be fact, episode, or relation)", memType)
	}
	// ... rest unchanged
```

Add `"fmt"` to imports (already there from `decayFactor`? check — no, currently no fmt in memory.go).

- [ ] **Step 2: Add test to memory_test.go**

```go
func TestMemoryAddInvalidType(t *testing.T) {
	initMemoryTestDB(t)
	svc := NewMemoryService(nil, nil)

	_, err := svc.Add("test", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if !strings.Contains(err.Error(), "invalid memory type") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

Add `"strings"` to imports in memory_test.go.

- [ ] **Step 3: Run tests**

```bash
go test -tags fts5 -mod=mod ./internal/service/ -v -run "TestMemoryAdd"
```

Expected: All pass including new TestMemoryAddInvalidType

- [ ] **Step 4: Commit**

```bash
git add internal/service/memory.go internal/service/memory_test.go && git commit -m "fix: validate memory type in Add (fact|episode|relation)"
```

---

### Task 5: Merge defaults in config Load

**Problem:** If user's config.yaml has only partial fields, the rest are zero-valued instead of defaults. E.g. `daemon: {port: 19999}` makes embedding model empty string.

**Files:**
- Modify: `internal/config/config.go` (Load function)
- Modify: `internal/config/config_test.go` (add test)

- [ ] **Step 1: Rewrite Load to merge with defaults**

```go
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

	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	Cfg = cfg
	return cfg, nil
}
```

Key change: `var cfg Config` → `cfg := DefaultConfig()`, then Unmarshal into the defaults.

- [ ] **Step 2: Add test**

```go
func TestLoadPartialConfig(t *testing.T) {
	dir := t.TempDir()
	origDir := configDir
	configDir = dir
	defer func() { configDir = origDir }()

	os.WriteFile(filepath.Join(dir, "config.yaml"), []byte("daemon:\n  port: 19999\n"), 0644)

	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Daemon.Port != 19999 {
		t.Fatalf("expected port 19999, got %d", loaded.Daemon.Port)
	}
	if loaded.Embedding.Ollama.Model != "qwen3-embedding:0.6b-q8_0" {
		t.Fatalf("expected default embedding model, got %q", loaded.Embedding.Ollama.Model)
	}
	if loaded.Embedding.BatchSize != 8 {
		t.Fatalf("expected default batch_size 8, got %d", loaded.Embedding.BatchSize)
	}
	if !loaded.HyDE.Enabled {
		t.Fatal("expected default HyDE enabled=true")
	}
}
```

- [ ] **Step 3: Run tests**

```bash
go test -tags fts5 -mod=mod ./internal/config/ -v
```

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go && git commit -m "fix: merge user config with defaults so partial YAML doesn't zero out fields"
```

---

### Task 6: Fix embedWorker backoff

**Problem:** The `default` branch in select runs immediately (no blocking). If EmbedBatch returns empty result without error, infinite loop with no sleep.

**Files:**
- Modify: `internal/daemon/daemon.go` (embedWorker method)

- [ ] **Step 1: Rewrite embedWorker with ticker**

```go
func (my *Daemon) embedWorker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-my.done:
			return
		case <-ticker.C:
			count := dao.GetUnembeddedCount()
			if count == 0 {
				continue
			}
			_, err := my.embedder.EmbedBatch(context.Background(), 0)
			if err != nil {
				logo.Error("embedWorker: %s", err)
			}
		}
	}
}
```

- [ ] **Step 2: Build and test**

```bash
go build -tags fts5 -mod=mod ./... && go test -tags fts5 -mod=mod ./...
```

- [ ] **Step 3: Commit**

```bash
git add internal/daemon/daemon.go && git commit -m "fix: embedWorker use ticker instead of busy loop"
```

---

## Chunk 3: Low Priority — Cleanup

### Task 7: Remove unused --index flag and clean FuseResults

**Files:**
- Modify: `internal/cli/root.go`
- Modify: `internal/service/fusion.go`

- [ ] **Step 1: Remove --index flag from root.go**

Delete the `indexPath` variable and the `rootCmd.PersistentFlags().StringVar` line for it.

- [ ] **Step 2: Clean FuseResults signature**

```go
func FuseResults(lexHits, vecHits []formatter.SearchHit) []formatter.SearchHit {
	lists := [][]formatter.SearchHit{lexHits, vecHits}
	return ReciprocalRankFusion(lists, DefaultRRFParams())
}
```

Remove the unused `vectorWeight float64` parameter. Update caller in `searcher.go:116`:

```go
fused := FuseResults(lexHits, vecHits)
```

And in `routes.go` handleQuery (if the fallback `FuseResults(lexHits, vecHits)` call exists from Task 1).

- [ ] **Step 3: Build and test**

```bash
go build -tags fts5 -mod=mod ./... && go test -tags fts5 -mod=mod ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/cli/root.go internal/service/fusion.go internal/service/searcher.go && git commit -m "cleanup: remove unused --index flag, clean FuseResults signature"
```

---

### Task 8: Decide on memory embedding storage

**Problem:** Memory records store embeddings but there's no vector search for memories. The embeddings are never used. This wastes disk space and adds latency on every `memory_add`.

**Decision needed from human:** Two options:
- **A. Remove embedding storage** — delete the embedding logic from `MemoryService.Add`, remove the `embedding BLOB` column from schema, remove `UpdateMemoryEmbedding`. Simpler. Memory search is FTS-only.
- **B. Keep embedding storage for future vector search** — leave as-is, accept the cost. Add a TODO comment.

If A, add a test that verifies `Add` works without provider and no embedding stored.

- [ ] **Step depends on human decision**

---

## Final Verification

After all tasks:

```bash
go build -tags fts5 -mod=mod ./... && go test -tags fts5 -mod=mod ./... && go vet -tags fts5 -mod=mod ./...
```

Expected: All pass, no warnings.
