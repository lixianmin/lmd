# Store → DAO Refactor Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename `internal/store` to `internal/dao`, introduce a global `dao.DB` Store object, remove `db *sql.DB` as first parameter from all functions.

**Architecture:** Create `dao.Store` struct holding `*sql.DB`, expose it as package-level `dao.DB`. All dao functions become methods on `*Store` or package-level functions using `DB.db`. CLI layer initializes once via `dao.Init()`, no more per-command `openDB()/Close()`.

**Tech Stack:** Go, sqlite3, cobra

---

## Chunk 1: Rename package and introduce Store object

### Task 1: Create dao directory and store.go

**Files:**
- Create: `internal/dao/store.go`

- [ ] **Step 1: Create `internal/dao/store.go`**

```go
package dao

import (
	"database/sql"

	"github.com/lixianmin/lmd/internal/store"
)

var DB *Store

type Store struct {
	db *sql.DB
}

func Init(dbPath string) error {
	db, err := store.OpenAndInit(dbPath)
	if err != nil {
		return err
	}
	DB = &Store{db: db}
	return nil
}
```

Note: Initially wraps old `store` package so we can migrate incrementally. After all callers are migrated, we'll move code from `store` to `dao` and delete `store`.

- [ ] **Step 2: Verify it compiles**

Run: `go build -tags "fts5" ./internal/dao/...`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/dao/store.go
git commit -m "refactor: create dao package with Store wrapper"
```

### Task 2: Initialize dao.DB in CLI root

**Files:**
- Modify: `cmd/lmd/main.go`
- Modify: `internal/cli/root.go`

- [ ] **Step 1: Add `dao.Init()` to root.go PersistentPreRunE**

In `internal/cli/root.go`, add import `"github.com/lixianmin/lmd/internal/dao"`. Add `PersistentPreRunE` to `rootCmd`:

```go
var rootCmd = &cobra.Command{
	Use:   "lmd",
	Short: "LMD - Local Markdown Docs search engine",
	Long:  "A local hybrid search engine for Markdown documents with Chinese language support.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return dao.Init(getDefaultIndexPath())
	},
}
```

Remove the `openDB()` function entirely.

- [ ] **Step 2: Update main.go to close on exit**

In `cmd/lmd/main.go`, add `defer dao.DB.Close()` after `cli.Execute()` (or wrap it).

- [ ] **Step 3: Verify it compiles**

Run: `go build -tags "fts5" ./...`
Expected: compile errors from all the `openDB()` callers — that's expected, we'll fix in next tasks.

---

## Chunk 2: Migrate CLI commands to use dao.DB

Migrate each CLI file: remove `openDB()/defer db.Close()`, replace `store.Xxx(db, ...)` with `dao.DB.Xxx(...)`.

Since `dao.Store` doesn't have methods yet, callers will use `dao.DB` to get the underlying db. During this transitional phase, each CLI command does:

```go
db := dao.DB.DB()  // or expose a getter
result, err := store.GetDocumentByDocId(db, docId)
```

Wait — this is messy. Better approach: move all `store` code into `dao` in one go, then update callers.

### Revised approach: Move all store code, then update callers

### Task 3: Move all files from internal/store to internal/dao

**Files:**
- Move: `internal/store/*.go` → `internal/dao/*.go`
- Delete: `internal/store/` directory

- [ ] **Step 1: Copy all .go files from store to dao, change package name**

For each file in `internal/store/`:
1. Copy to `internal/dao/`
2. Change `package store` → `package dao`
3. Remove `db *sql.DB` as first parameter from ALL functions
4. Replace all `db.` references inside functions with `DB.db` (the global)

Functions to modify (removing first `db *sql.DB` param):

**db.go:**
- `OpenAndInit(dbPath string) (*sql.DB, error)` — keep as-is (used by Init)
- Add `Init(dbPath string) error` that sets `DB`

**schema.go:**
- `CreateTables(db *sql.DB) error` → `createTables() error` (called internally)

**document.go:**
- `UpsertDocument(db, *DocumentRecord) error` → `UpsertDocument(*DocumentRecord) error`
- `GetDocumentByID(db, int64)` → `GetDocumentByID(int64)`
- `GetDocumentByDocId(db, string)` → `GetDocumentByDocId(string)`
- `GetDocumentByPath(db, string, string)` → `GetDocumentByPath(string, string)`
- `ListDocumentsByCollection(db, string)` → `ListDocumentsByCollection(string)`
- `DeleteDocument(db, int64)` → `DeleteDocument(int64)`
- `SearchDocumentsByPath(db, string, string)` → `SearchDocumentsByPath(string, string)`
- `ShortDocId(string) string` — no db param, keep as-is
- `generateDocId(...)` — private, no db param, keep as-is

**collection.go:**
- `AddCollection(db, name, path, glob, ignore)` → `AddCollection(name, path, glob, ignore)`
- `RemoveCollection(db, name)` → `RemoveCollection(name)`
- `RenameCollection(db, old, new)` → `RenameCollection(old, new)`
- `ListCollections(db)` → `ListCollections()`

**context.go:**
- `AddContext(db, col, path, ctx)` → `AddContext(col, path, ctx)`
- `RemoveContext(db, col, path)` → `RemoveContext(col, path)`
- `ListContexts(db, col)` → `ListContexts(col)`

**fts.go:**
- `PrepareFTSStatements(db)` → `prepareFTSStatements()` (called internally by Init)
- `SearchFTS(db, query, collection, limit)` → `SearchFTS(query, collection, limit)`

**vector.go:**
- `InsertChunks(db, docId, chunks, tokenized)` → `InsertChunks(docId, chunks, tokenized)`
- `InsertVector(db, chunkId, vec)` → `InsertVector(chunkId, vec)`
- `QueryVectors(db, queryVec, limit)` → `QueryVectors(queryVec, limit)`
- `DeleteVectorsByDocId(db, docId)` → `DeleteVectorsByDocId(docId)`
- `GetUnembeddedChunks(db, limit)` → `GetUnembeddedChunks(limit)`
- `GetChunksByDocId(db, docId)` → `GetChunksByDocId(docId)`
- `GetChunkByID(db, id)` → `GetChunkByID(id)`
- `SimilarityToScore(distance)` — no db, keep as-is

**stats.go (NEW):**
Add new file to centralize all stats/count queries currently scattered in service and cli:
- `GetChunkCounts() (total, embedded int)` — `SELECT COUNT(*) FROM chunks` + `SELECT COUNT(*) FROM chunks_vec_rowids`
- `GetUnembeddedCount() int` — `SELECT COUNT(*) FROM chunks c LEFT JOIN chunks_vec v ON c.id = v.chunk_id WHERE v.chunk_id IS NULL`

These replace:
- `service/embedder.go:45-46` — inline `db.QueryRow("SELECT COUNT(*) FROM chunks")`
- `cli/index.go:108-111` — inline `db.QueryRow("SELECT COUNT(*) FROM chunks")`
- `cli/search.go:70` — inline `db.QueryRow("SELECT COUNT(*) FROM chunks c LEFT JOIN chunks_vec")`

**store.go** (new global):

```go
package dao

import "database/sql"

var DB *Store

type Store struct {
	db *sql.DB
}

func Init(dbPath string) error {
	var err error
	DB = &Store{}
	DB.db, err = OpenAndInit(dbPath)
	if err != nil {
		return err
	}
	if err := createTables(); err != nil {
		return err
	}
	return prepareFTSStatements()
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
```

In every function body, replace `db.Prepare(` → `DB.db.Prepare(`, `db.QueryRow(` → `DB.db.QueryRow(`, etc.

- [ ] **Step 2: Verify dao package compiles**

Run: `go build -tags "fts5" ./internal/dao/...`
Expected: PASS (no external callers yet)

- [ ] **Step 3: Commit**

```bash
git add internal/dao/
git commit -m "refactor: move store to dao, remove db parameter from all functions"
```

### Task 4: Update all callers (CLI layer)

**Files:**
- Modify: `internal/cli/root.go` — remove `openDB()`, add `dao.Init` in PersistentPreRunE
- Modify: `internal/cli/collection.go` — remove openDB/close, change `store.Xxx(db,` → `dao.Xxx(`
- Modify: `internal/cli/context.go` — same
- Modify: `internal/cli/embed.go` — same
- Modify: `internal/cli/get.go` — same
- Modify: `internal/cli/index.go` — same (note: rebuild closes/reopens db, needs special handling)
- Modify: `internal/cli/mcp.go` — same
- Modify: `internal/cli/search.go` — same
- Modify: `cmd/lmd/main.go` — add defer dao.DB.Close()

For each file the pattern is:
1. Replace import `"github.com/lixianmin/lmd/internal/store"` → `"github.com/lixianmin/lmd/internal/dao"`
2. Remove `db, err := openDB()` + error check + `defer db.Close()`
3. Change `store.Xxx(db,` → `dao.Xxx(`
4. Change `store.ShortDocId(` → `dao.ShortDocId(` (functions that never took db)

Special case — `index.go` rebuild command: currently closes db, deletes file, reopens. Change to:
```go
dao.DB.Close()
os.Remove(dbPath)
dao.Init(dbPath) // reopens
```

- [ ] **Step 1: Update root.go and main.go**

- [ ] **Step 2: Update collection.go, context.go, get.go**

- [ ] **Step 3: Update search.go, embed.go, mcp.go**

- [ ] **Step 4: Update index.go (including rebuild special case)**

- [ ] **Step 5: Verify full build**

Run: `go build -tags "fts5" ./...`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git commit -am "refactor: CLI layer uses dao.DB global, no more openDB/close per command"
```

### Task 5: Update service layer

**Files:**
- Modify: `internal/service/indexer.go` — remove `db *sql.DB` from Indexer struct
- Modify: `internal/service/searcher.go` — remove `db *sql.DB` from Searcher struct
- Modify: `internal/service/embedder.go` — remove `db *sql.DB` from Embedder struct, replace inline SQL with `dao.GetChunkCounts()`
- Modify: `internal/service/fusion.go` — no db, just types, maybe no change needed

Pattern: each service struct currently holds `db *sql.DB`. Remove it. Replace `s.db.` calls with `dao.Xxx()` calls. Update constructors.

Before:
```go
type Indexer struct { db *sql.DB; tok tokenizer.Tokenizer }
func NewIndexer(db *sql.DB, tok tokenizer.Tokenizer) *Indexer {
    return &Indexer{db: db, tok: tok}
}
```

After:
```go
type Indexer struct { tok tokenizer.Tokenizer }
func NewIndexer(tok tokenizer.Tokenizer) *Indexer {
    return &Indexer{tok: tok}
}
```

**Inline SQL to move from embedder.go to dao/stats.go:**

Before (embedder.go):
```go
e.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&totalChunks)
e.db.QueryRow("SELECT COUNT(*) FROM chunks_vec_rowids").Scan(&embeddedCount)
```

After:
```go
totalChunks, embeddedCount := dao.GetChunkCounts()
```

**Inline SQL to move from cli/search.go to dao/stats.go:**

Before (search.go syncEmbeddings):
```go
db.QueryRow(`SELECT COUNT(*) FROM chunks c LEFT JOIN chunks_vec v ON c.id = v.chunk_id WHERE v.chunk_id IS NULL`).Scan(&unembeddedCount)
```

After:
```go
unembeddedCount := dao.GetUnembeddedCount()
```

**Inline SQL to move from cli/index.go to dao/stats.go:**

Before (index.go status command):
```go
db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunkCount)
db.QueryRow("SELECT COUNT(*) FROM chunks_vec_rowids").Scan(&embedCount)
```

After:
```go
chunkCount, embedCount := dao.GetChunkCounts()
```

Update CLI callers: `service.NewIndexer(db, tok)` → `service.NewIndexer(tok)`.

- [ ] **Step 1: Update indexer.go**

- [ ] **Step 2: Update searcher.go**

- [ ] **Step 3: Update embedder.go**

- [ ] **Step 4: Update CLI callers of these constructors**

- [ ] **Step 5: Verify build**

Run: `go build -tags "fts5" ./...`

- [ ] **Step 6: Commit**

```bash
git commit -am "refactor: service layer uses dao package directly"
```

### Task 6: Update pkg/ public API

**Files:**
- Modify: `pkg/lmd.go`

Update `LmdStore` struct and its methods to use `dao` instead of `store`.

- [ ] **Step 1: Update pkg/lmd.go**

- [ ] **Step 2: Verify build**

Run: `go build -tags "fts5" ./...`

- [ ] **Step 3: Commit**

```bash
git commit -am "refactor: update public pkg API to use dao"
```

### Task 7: Update tests

**Files:**
- Move: `internal/store/*_test.go` → `internal/dao/*_test.go`
- Modify: `internal/service/*_test.go`
- Modify: `internal/formatter/*_test.go`
- Modify: `pkg/*_test.go`

Test migration pattern:
1. Change `package store_test` → `package dao_test`
2. Change `store.Xxx(db,` → `dao.Xxx(`
3. Test setup: instead of creating db and passing it, call `dao.Init(tmpDbPath)` or directly set `dao.DB = &dao.Store{db: testDb}`
4. Tests that need isolation should create a temp Store, not use the global

Add helper in `dao/store_test_helper.go`:
```go
func initTestDB(t *testing.T) {
    t.Helper()
    dir := t.TempDir()
    dbPath := filepath.Join(dir, "test.sqlite")
    if err := Init(dbPath); err != nil {
        t.Fatal(err)
    }
    t.Cleanup(func() { DB.Close(); DB = nil })
}
```

- [ ] **Step 1: Migrate dao tests (formerly store tests)**

- [ ] **Step 2: Update service tests**

- [ ] **Step 3: Update formatter and pkg tests**

- [ ] **Step 4: Run all tests**

Run: `go test -tags "fts5" ./...`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git commit -am "refactor: migrate all tests to dao package"
```

### Task 8: Delete old store package and update AGENTS.md

**Files:**
- Delete: `internal/store/` directory
- Modify: `AGENTS.md`

- [ ] **Step 1: Delete internal/store/**

- [ ] **Step 2: Update AGENTS.md** — replace all references to `internal/store/` with `internal/dao/`

- [ ] **Step 3: Final verification**

Run: `go build -tags "fts5" ./... && go test -tags "fts5" ./... && make vet`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
git commit -am "refactor: delete old store package, update docs"
```
