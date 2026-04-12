# Phase 1 - Foundation Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the foundation layer — Go module, SQLite store, gse tokenizer, BM25 keyword search, basic CLI commands (collection, update, search, get, status).

**Architecture:** Layered pipeline (CLI → Service → Store). Store manages SQLite with FTS5 for full-text search. Tokenizer uses gse for Chinese/English segmentation. CLI built with cobra.

**Tech Stack:** Go 1.22+, mattn/go-sqlite3 (CGo), go-ego/gse, spf13/cobra, log/slog

**Spec:** `docs/superpowers/specs/2026-04-12-lmd-design.md`

---

## Chunk 1: Project Scaffold & Go Module

### Task 1: Initialize Go module and directory structure

**Files:**
- Create: `go.mod`
- Create: `cmd/lmd/main.go`

- [ ] **Step 1: Initialize Go module**

Run:
```bash
cd /Users/xmli/me/code/lmd
go mod init github.com/lixianmin/lmd
```

- [ ] **Step 2: Create directory structure**

Run:
```bash
mkdir -p cmd/lmd internal/cli internal/service internal/store internal/tokenizer internal/chunker internal/embedding internal/formatter pkg test/fixtures
```

- [ ] **Step 3: Create minimal main.go**

Create `cmd/lmd/main.go`:
```go
package main

import (
	"os"

	"github.com/lixianmin/lmd/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
```

- [ ] **Step 4: Add cobra dependency**

Run:
```bash
go get github.com/spf13/cobra@latest
```

- [ ] **Step 5: Create root command stub**

Create `internal/cli/root.go`:
```go
package cli

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/spf13/cobra"
)

var (
	indexPath string
	verbose   bool
)

var rootCmd = &cobra.Command{
	Use:   "lmd",
	Short: "LMD - Local Markdown Docs search engine",
	Long:  "A local hybrid search engine for Markdown documents with Chinese language support.",
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&indexPath, "index", "", "database file path (default: ~/.cache/lmd/index.sqlite)")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable debug-level logging")
	rootCmd.Version = "0.1.0"
}

func getDefaultIndexPath() string {
	if indexPath != "" {
		return indexPath
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "lmd.sqlite"
	}
	return filepath.Join(home, ".cache", "lmd", "index.sqlite")
}

func openDB() (*sql.DB, error) {
	return store.OpenAndMigrate(getDefaultIndexPath())
}
```

- [ ] **Step 6: Verify build**

Run:
```bash
go build ./cmd/lmd/
```
Expected: builds without errors

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: initialize Go module and project scaffold"
```

---

## Chunk 2: Store Layer — DB Connection & Schema

### Task 2: SQLite connection management

**Files:**
- Create: `internal/store/db.go`
- Test: `internal/store/db_test.go`

- [ ] **Step 1: Write failing test for OpenDB**

Create `internal/store/db_test.go`:
```go
package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenDBCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestOpenDBEnablesWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %s", journalMode)
	}
}

func TestOpenDBCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "nested", "test.sqlite")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created in nested directory")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestOpenDB -v`
Expected: FAIL — `OpenDB` undefined

- [ ] **Step 3: Install go-sqlite3 dependency and implement OpenDB**

Run:
```bash
go get github.com/mattn/go-sqlite3@latest
```

Create `internal/store/db.go`:
```go
package store

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func OpenDB(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=wal&_foreign_keys=on")
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}
```

Also add `OpenAndMigrate` to `internal/store/db.go`:
```go
func OpenAndMigrate(dbPath string) (*sql.DB, error) {
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestOpenDB -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add SQLite connection management with WAL mode"
```

### Task 3: Schema migration

**Files:**
- Create: `internal/store/schema.go`
- Test: `internal/store/schema_test.go`

- [ ] **Step 1: Write failing test for schema migration**

Create `internal/store/schema_test.go`:
```go
package store

import (
	"database/sql"
	"testing"
)

func TestMigrateCreatesTables(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	tables := []string{"collections", "path_contexts", "documents", "chunks", "embed_status", "_meta"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate failed: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}
}

func TestMigrateSetsVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	var version string
	err := db.QueryRow("SELECT value FROM _meta WHERE key='schema_version'").Scan(&version)
	if err != nil {
		t.Fatalf("failed to query schema_version: %v", err)
	}
	if version != "1" {
		t.Fatalf("expected schema_version=1, got %s", version)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(dir + "/test.sqlite")
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	return db
}
```

Note: add `"database/sql"` import to `schema_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestMigrate -v`
Expected: FAIL — `Migrate` undefined

- [ ] **Step 3: Implement Migrate**

Create `internal/store/schema.go`:
```go
package store

import (
	"database/sql"
)

type Migration struct {
	Version int
	Up      func(tx *sql.Tx) error
}

var migrations = []Migration{
	{Version: 1, Up: migrateV1},
}

func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS _meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return err
	}

	var current int
	row := db.QueryRow("SELECT CAST(value AS INTEGER) FROM _meta WHERE key='schema_version'")
	if err := row.Scan(&current); err != nil {
		if err == sql.ErrNoRows {
			current = 0
		} else {
			return err
		}
	}

	for _, m := range migrations {
		if m.Version > current {
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			if err := m.Up(tx); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.Exec(
				"INSERT OR REPLACE INTO _meta (key, value) VALUES ('schema_version', ?)", m.Version,
			); err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateV1(tx *sql.Tx) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS collections (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL UNIQUE,
			path            TEXT NOT NULL,
			glob_pattern    TEXT DEFAULT '**/*.md',
			ignore_patterns TEXT,
			created_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS path_contexts (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			collection  TEXT NOT NULL,
			path        TEXT NOT NULL DEFAULT '',
			context     TEXT NOT NULL,
			UNIQUE(collection, path)
		)`,
		`CREATE TABLE IF NOT EXISTS documents (
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
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			tokens,
			title_tokens,
			content='documents',
			content_rowid='id',
			tokenize='unicode61'
		)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id      INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			seq         INTEGER NOT NULL,
			content     TEXT NOT NULL,
			position    INTEGER NOT NULL,
			token_count INTEGER,
			hash        TEXT NOT NULL,
			UNIQUE(doc_id, seq)
		)`,
		`CREATE TABLE IF NOT EXISTS embed_status (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			chunk_id    INTEGER NOT NULL REFERENCES chunks(id) ON DELETE CASCADE,
			model_name  TEXT NOT NULL,
			embedded_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(chunk_id, model_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_collection ON documents(collection)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_hash ON documents(hash)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/store/ -run TestMigrate -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add schema migration with all Phase 1 tables"
```

---

## Chunk 3: Store Layer — Collection CRUD

### Task 4: Collection persistence

**Files:**
- Create: `internal/store/collection.go`
- Test: `internal/store/collection_test.go`

- [ ] **Step 1: Write failing tests for Collection CRUD**

Create `internal/store/collection_test.go`:
```go
package store

import (
	"database/sql"
	"testing"
)

func TestAddCollection(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	err := AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	if err != nil {
		t.Fatalf("AddCollection failed: %v", err)
	}

	name, path, glob := getCollection(t, db, "notes")
	if name != "notes" || path != "/home/user/notes" || glob != "**/*.md" {
		t.Fatalf("unexpected values: name=%s path=%s glob=%s", name, path, glob)
	}
}

func TestAddCollectionDuplicate(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	err := AddCollection(db, "notes", "/home/user/other", "**/*.md", nil)
	if err == nil {
		t.Fatal("expected error for duplicate collection name")
	}
}

func TestRemoveCollection(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	if err := RemoveCollection(db, "notes"); err != nil {
		t.Fatalf("RemoveCollection failed: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM collections WHERE name='notes'").Scan(&count)
	if count != 0 {
		t.Fatal("collection should be removed")
	}
}

func TestRemoveCollectionNotFound(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	err := RemoveCollection(db, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent collection")
	}
}

func TestListCollections(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	_ = AddCollection(db, "docs", "/home/user/docs", "**/*.md", nil)

	cols, err := ListCollections(db)
	if err != nil {
		t.Fatalf("ListCollections failed: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(cols))
	}

	names := map[string]bool{}
	for _, c := range cols {
		names[c.Name] = true
	}
	if !names["notes"] || !names["docs"] {
		t.Fatal("missing expected collections")
	}
}

func TestRenameCollection(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	if err := RenameCollection(db, "notes", "my-notes"); err != nil {
		t.Fatalf("RenameCollection failed: %v", err)
	}

	cols, _ := ListCollections(db)
	for _, c := range cols {
		if c.Name == "notes" {
			t.Fatal("old name should not exist")
		}
		if c.Name == "my-notes" {
			return
		}
	}
	t.Fatal("renamed collection not found")
}

func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	if err := Migrate(db); err != nil {
		db.Close()
		t.Fatalf("migration failed: %v", err)
	}
	return db
}

func getCollection(t *testing.T, db *sql.DB, name string) (string, string, string) {
	t.Helper()
	var n, p, g string
	err := db.QueryRow("SELECT name, path, glob_pattern FROM collections WHERE name=?", name).Scan(&n, &p, &g)
	if err != nil {
		t.Fatalf("failed to get collection %s: %v", name, err)
	}
	return n, p, g
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestAddCollection -v`
Expected: FAIL — `AddCollection` undefined

- [ ] **Step 3: Implement Collection CRUD**

Create `internal/store/collection.go`:
```go
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type CollectionRecord struct {
	ID             int
	Name           string
	Path           string
	GlobPattern    string
	IgnorePatterns []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DocCount       int
}

func AddCollection(db *sql.DB, name, path, globPattern string, ignorePatterns []string) error {
	var ignoreJSON *string
	if len(ignorePatterns) > 0 {
		b, err := json.Marshal(ignorePatterns)
		if err != nil {
			return err
		}
		s := string(b)
		ignoreJSON = &s
	}

	_, err := db.Exec(
		"INSERT INTO collections (name, path, glob_pattern, ignore_patterns) VALUES (?, ?, ?, ?)",
		name, path, globPattern, ignoreJSON,
	)
	return err
}

func RemoveCollection(db *sql.DB, name string) error {
	res, err := db.Exec("DELETE FROM collections WHERE name=?", name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("collection not found: " + name)
	}
	return nil
}

func ListCollections(db *sql.DB) ([]CollectionRecord, error) {
	rows, err := db.Query(`
		SELECT c.id, c.name, c.path, c.glob_pattern, c.ignore_patterns,
		       c.created_at, c.updated_at,
		       COUNT(d.id) AS doc_count
		FROM collections c
		LEFT JOIN documents d ON d.collection = c.name
		GROUP BY c.id
		ORDER BY c.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []CollectionRecord
	for rows.Next() {
		var c CollectionRecord
		var ignoreJSON *string
		var docCount int
		if err := rows.Scan(&c.ID, &c.Name, &c.Path, &c.GlobPattern, &ignoreJSON,
			&c.CreatedAt, &c.UpdatedAt, &docCount); err != nil {
			return nil, err
		}
		if ignoreJSON != nil {
			json.Unmarshal([]byte(*ignoreJSON), &c.IgnorePatterns)
		}
		c.DocCount = docCount
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func RenameCollection(db *sql.DB, oldName, newName string) error {
	res, err := db.Exec("UPDATE collections SET name=?, updated_at=CURRENT_TIMESTAMP WHERE name=?", newName, oldName)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("collection not found: " + oldName)
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'Test(Add|Remove|List|Rename)Collection' -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add collection CRUD in store layer"
```

---

## Chunk 4: Store Layer — Document CRUD & FTS5

### Task 5: Document persistence

**Files:**
- Create: `internal/store/document.go`
- Create: `internal/store/fts.go`
- Test: `internal/store/document_test.go`

- [ ] **Step 1: Write failing tests for Document CRUD**

Create `internal/store/document_test.go`:
```go
package store

import (
	"testing"
)

func TestUpsertDocument(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "Test Document",
		Body:       "Hello world",
		Hash:       "abc123",
		FileSize:   100,
	}
	err := UpsertDocument(db, &doc, "hello world", "test document")
	if err != nil {
		t.Fatalf("UpsertDocument failed: %v", err)
	}

	if doc.DocID == "" {
		t.Fatal("docid should be set")
	}
	if doc.ID == 0 {
		t.Fatal("id should be set")
	}
}

func TestUpsertDocumentUpdate(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc1 := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "V1",
		Body:       "body v1",
		Hash:       "hash1",
		FileSize:   10,
	}
	_ = UpsertDocument(db, &doc1, "body v1", "v1")

	doc2 := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "V2",
		Body:       "body v2",
		Hash:       "hash2",
		FileSize:   20,
	}
	_ = UpsertDocument(db, &doc2, "body v2", "v2")

	docs, _ := ListDocumentsByCollection(db, "notes")
	if len(docs) != 1 {
		t.Fatalf("expected 1 document (updated), got %d", len(docs))
	}
	if docs[0].Title != "V2" {
		t.Fatalf("expected title V2, got %s", docs[0].Title)
	}
}

func TestGetDocumentByDocID(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "Test",
		Body:       "content",
		Hash:       "hash1",
	}
	_ = UpsertDocument(db, &doc, "content", "test")

	got, err := GetDocumentByDocID(db, doc.DocID)
	if err != nil {
		t.Fatalf("GetDocumentByDocID failed: %v", err)
	}
	if got.Path != "test.md" {
		t.Fatalf("expected path test.md, got %s", got.Path)
	}
}

func TestGetDocumentByPath(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "sub/test.md",
		Title:      "Test",
		Body:       "content",
		Hash:       "hash1",
	}
	_ = UpsertDocument(db, &doc, "content", "test")

	got, err := GetDocumentByPath(db, "notes", "sub/test.md")
	if err != nil {
		t.Fatalf("GetDocumentByPath failed: %v", err)
	}
	if got.DocID != doc.DocID {
		t.Fatalf("expected docid %s, got %s", doc.DocID, got.DocID)
	}
}

func TestDeleteDocument(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "Test",
		Body:       "content",
		Hash:       "hash1",
	}
	_ = UpsertDocument(db, &doc, "content", "test")

	if err := DeleteDocument(db, doc.ID); err != nil {
		t.Fatalf("DeleteDocument failed: %v", err)
	}

	_, err := GetDocumentByDocID(db, doc.DocID)
	if err == nil {
		t.Fatal("expected error for deleted document")
	}
}

func TestGetDocumentHashByPath(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "Test",
		Body:       "content",
		Hash:       "hash1",
	}
	_ = UpsertDocument(db, &doc, "content", "test")

	hash, err := GetDocumentHash(db, "notes", "test.md")
	if err != nil {
		t.Fatalf("GetDocumentHash failed: %v", err)
	}
	if hash != "hash1" {
		t.Fatalf("expected hash hash1, got %s", hash)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestUpsertDocument -v`
Expected: FAIL

- [ ] **Step 3: Implement Document CRUD**

Create `internal/store/document.go`:
```go
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type DocumentRecord struct {
	ID         int64
	DocID      string
	Collection string
	Path       string
	Title      string
	Body       string
	Hash       string
	FileSize   int64
	ModifiedAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func generateDocID(collection, path, hash string) string {
	raw := fmt.Sprintf("%s:%s:%s", collection, path, hash)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:3])
}

func UpsertDocument(db *sql.DB, doc *DocumentRecord, tokenizedBody, tokenizedTitle string) error {
	doc.DocID = generateDocID(doc.Collection, doc.Path, doc.Hash)

	var existingID int64
	err := db.QueryRow(
		"SELECT id FROM documents WHERE collection=? AND path=?",
		doc.Collection, doc.Path,
	).Scan(&existingID)

	if err == sql.ErrNoRows {
		res, err := db.Exec(
			`INSERT INTO documents (docid, collection, path, title, body, hash, file_size, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, DATETIME('now', '+8 hours'))`,
			doc.DocID, doc.Collection, doc.Path, doc.Title, doc.Body, doc.Hash, doc.FileSize,
		)
		if err != nil {
			return err
		}
		doc.ID, _ = res.LastInsertId()

		_, err = db.Exec(
			"INSERT INTO documents_fts (rowid, tokens, title_tokens) VALUES (?, ?, ?)",
			doc.ID, tokenizedBody, tokenizedTitle,
		)
		return err
	}

	if err != nil {
		return err
	}

	doc.ID = existingID
	_, err = db.Exec(
		`UPDATE documents SET docid=?, title=?, body=?, hash=?, file_size=?, modified_at=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP
		 WHERE id=?`,
		doc.DocID, doc.Title, doc.Body, doc.Hash, doc.FileSize, existingID,
	)
	if err != nil {
		return err
	}

	_, err = db.Exec(
		"UPDATE documents_fts SET tokens=?, title_tokens=? WHERE rowid=?",
		tokenizedBody, tokenizedTitle, existingID,
	)
	return err
}

func GetDocumentByDocID(db *sql.DB, docID string) (*DocumentRecord, error) {
	return getDocument(db, "WHERE docid=?", docID)
}

func GetDocumentByPath(db *sql.DB, collection, path string) (*DocumentRecord, error) {
	return getDocument(db, "WHERE collection=? AND path=?", collection, path)
}

func getDocument(db *sql.DB, whereClause string, args ...any) (*DocumentRecord, error) {
	query := "SELECT id, docid, collection, path, title, body, hash, file_size, modified_at, created_at FROM documents " + whereClause
	row := db.QueryRow(query, args...)

	var doc DocumentRecord
	err := row.Scan(&doc.ID, &doc.DocID, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
		&doc.Hash, &doc.FileSize, &doc.ModifiedAt, &doc.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("document not found")
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func DeleteDocument(db *sql.DB, id int64) error {
	_, err := db.Exec("DELETE FROM documents_fts WHERE rowid=?", id)
	if err != nil {
		return err
	}
	_, err = db.Exec("DELETE FROM documents WHERE id=?", id)
	return err
}

func ListDocumentsByCollection(db *sql.DB, collection string) ([]DocumentRecord, error) {
	rows, err := db.Query(
		"SELECT id, docid, collection, path, title, body, hash, file_size, modified_at, created_at FROM documents WHERE collection=?",
		collection,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.ID, &doc.DocID, &doc.Collection, &doc.Path, &doc.Title,
			&doc.Body, &doc.Hash, &doc.FileSize, &doc.ModifiedAt, &doc.CreatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func GetDocumentHash(db *sql.DB, collection, path string) (string, error) {
	var hash string
	err := db.QueryRow("SELECT hash FROM documents WHERE collection=? AND path=?", collection, path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", errors.New("document not found")
	}
	return hash, err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/store/ -run 'Test(Upsert|Get|Delete)Document' -v`
Expected: all PASS

- [ ] **Step 5: Write failing test for FTS5 search**

Append to `internal/store/document_test.go`:
```go
func TestSearchFTS(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	docs := []struct {
		path   string
		title  string
		tokens string
	}{
		{"go.md", "Go Language", "go golang 并发 编程 语言"},
		{"python.md", "Python Notes", "python 编程 数据 科学"},
		{"rust.md", "Rust Guide", "rust 系统 编程 安全 内存"},
	}
	for _, d := range docs {
		doc := DocumentRecord{Collection: "notes", Path: d.path, Title: d.title, Body: "body", Hash: d.path}
		_ = UpsertDocument(db, &doc, d.tokens, d.title)
	}

	results, err := SearchFTS(db, "编程 语言", "", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}

	paths := map[string]bool{}
	for _, r := range results {
		paths[r.Path] = true
	}
	if !paths["go.md"] {
		t.Fatal("expected go.md in results for '编程 语言'")
	}
}

func TestSearchFTSWithCollection(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)
	_ = AddCollection(db, "docs", "/docs", "**/*.md", nil)

	doc1 := DocumentRecord{Collection: "notes", Path: "test.md", Title: "搜索测试", Body: "body", Hash: "h1"}
	_ = UpsertDocument(db, &doc1, "搜索 测试 中文", "搜索 测试")

	doc2 := DocumentRecord{Collection: "docs", Path: "test.md", Title: "搜索文档", Body: "body", Hash: "h2"}
	_ = UpsertDocument(db, &doc2, "搜索 文档", "搜索 文档")

	results, _ := SearchFTS(db, "搜索", "notes", 10)
	for _, r := range results {
		if r.Collection != "notes" {
			t.Fatalf("expected only notes collection, got %s", r.Collection)
		}
	}
}
```

- [ ] **Step 6: Run test to verify it fails**

Run: `go test ./internal/store/ -run TestSearchFTS -v`
Expected: FAIL — `SearchFTS` undefined

- [ ] **Step 7: Implement FTS5 search**

Create `internal/store/fts.go`:
```go
package store

import (
	"database/sql"
	"math"
)

type FTSSearchResult struct {
	ID         int64
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
}

func SearchFTS(db *sql.DB, tokenizedQuery, collection string, limit int) ([]FTSSearchResult, error) {
	query := `
		SELECT d.id, d.docid, d.collection, d.path, d.title,
			   abs(rank) as raw_score
		FROM documents_fts f
		JOIN documents d ON d.id = f.rowid
		WHERE f.tokens MATCH ?
	`
	args := []any{tokenizedQuery}

	if collection != "" {
		query += " AND d.collection = ?"
		args = append(args, collection)
	}

	query += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FTSSearchResult
	for rows.Next() {
		var r FTSSearchResult
		if err := rows.Scan(&r.ID, &r.DocID, &r.Collection, &r.Path, &r.Title, &r.Score); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	if len(results) > 0 {
		topScore := results[0].Score
		for i := range results {
			results[i].Score = math.Min(results[i].Score/topScore, 1.0)
		}
	}

	return results, rows.Err()
}
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/store/ -run TestSearchFTS -v`
Expected: PASS

- [ ] **Step 9: Run all store tests**

Run: `go test ./internal/store/ -v`
Expected: all PASS

- [ ] **Step 10: Commit**

```bash
git add -A
git commit -m "feat: add document CRUD and FTS5 search in store layer"
```

---

## Chunk 5: Tokenizer

### Task 6: gse tokenizer implementation

**Files:**
- Create: `internal/tokenizer/tokenizer.go`
- Create: `internal/tokenizer/gse.go`
- Test: `internal/tokenizer/gse_test.go`

- [ ] **Step 1: Add gse dependency**

Run:
```bash
go get github.com/go-ego/gse@latest
```

- [ ] **Step 2: Write failing tests for tokenizer**

Create `internal/tokenizer/gse_test.go`:
```go
package tokenizer

import (
	"strings"
	"testing"
)

func newTestTokenizer(t *testing.T) *GseTokenizer {
	t.Helper()
	tok, err := NewGseTokenizer()
	if err != nil {
		t.Fatalf("failed to create tokenizer: %v", err)
	}
	return tok
}

func TestCutChinese(t *testing.T) {
	tok := newTestTokenizer(t)
	tokens := tok.Cut("搜索引擎支持中文检索")
	if len(tokens) == 0 {
		t.Fatal("expected tokens for Chinese text")
	}
	joined := strings.Join(tokens, " ")
	if !strings.Contains(joined, "搜索") || !strings.Contains(joined, "引擎") {
		t.Fatalf("expected key tokens, got: %v", tokens)
	}
}

func TestCutEnglish(t *testing.T) {
	tok := newTestTokenizer(t)
	tokens := tok.Cut("Hello World, this is a test")
	if len(tokens) == 0 {
		t.Fatal("expected tokens for English text")
	}
}

func TestCutMixed(t *testing.T) {
	tok := newTestTokenizer(t)
	tokens := tok.Cut("Go语言实现搜索引擎")
	if len(tokens) == 0 {
		t.Fatal("expected tokens for mixed text")
	}
	joined := strings.Join(tokens, " ")
	if !strings.Contains(joined, "搜索") || !strings.Contains(joined, "引擎") {
		t.Fatalf("expected key Chinese tokens, got: %v", tokens)
	}
}

func TestCutEmpty(t *testing.T) {
	tok := newTestTokenizer(t)
	tokens := tok.Cut("")
	if len(tokens) != 0 {
		t.Fatalf("expected empty result for empty input, got: %v", tokens)
	}
}

func TestCutForSearch(t *testing.T) {
	tok := newTestTokenizer(t)
	normal := tok.Cut("搜索引擎")
	search := tok.CutForSearch("搜索引擎")
	if len(search) < len(normal) {
		t.Fatalf("search mode should produce at least as many tokens as normal mode, got normal=%d search=%d", len(normal), len(search))
	}
}

func TestTokenizeToString(t *testing.T) {
	tok := newTestTokenizer(t)
	result := tok.TokenizeToString("搜索引擎支持中文")
	if result == "" {
		t.Fatal("expected non-empty result")
	}
	parts := strings.Split(result, " ")
	if len(parts) < 2 {
		t.Fatalf("expected at least 2 space-separated tokens, got: %s", result)
	}
}

func TestTokenizeToStringEmpty(t *testing.T) {
	tok := newTestTokenizer(t)
	result := tok.TokenizeToString("")
	if result != "" {
		t.Fatalf("expected empty string for empty input, got: %s", result)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/tokenizer/ -run TestCut -v`
Expected: FAIL — `GseTokenizer` undefined

- [ ] **Step 4: Define Tokenizer interface**

Create `internal/tokenizer/tokenizer.go`:
```go
package tokenizer

type Tokenizer interface {
	Cut(text string) []string
	CutForSearch(text string) []string
	TokenizeToString(text string) string
}
```

- [ ] **Step 5: Implement GseTokenizer**

Create `internal/tokenizer/gse.go`:
```go
package tokenizer

import (
	"strings"

	"github.com/go-ego/gse"
)

type GseTokenizer struct {
	seg *gse.Segmenter
}

func NewGseTokenizer() (*GseTokenizer, error) {
	var seg gse.Segmenter
	if err := seg.LoadDict("zh"); err != nil {
		return nil, err
	}
	return &GseTokenizer{seg: &seg}, nil
}

func (t *GseTokenizer) Cut(text string) []string {
	if text == "" {
		return nil
	}
	return t.seg.Cut(text)
}

func (t *GseTokenizer) CutForSearch(text string) []string {
	if text == "" {
		return nil
	}
	return t.seg.CutSearch(text)
}

func (t *GseTokenizer) TokenizeToString(text string) string {
	tokens := t.Cut(text)
	if len(tokens) == 0 {
		return ""
	}
	return strings.Join(tokens, " ")
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tokenizer/ -v`
Expected: all PASS

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "feat: add gse tokenizer with Chinese/English support"
```

---

## Chunk 6: Service Layer — Indexer & Searcher

### Task 7: Indexer service

**Files:**
- Create: `internal/service/indexer.go`
- Test: `internal/service/indexer_test.go`

- [ ] **Step 1: Create test fixtures**

Create `test/fixtures/simple.md`:
```markdown
# Simple Test

这是一个简单的测试文档。

## 第二段

Hello World, mixed content here.
```

Create `test/fixtures/chinese.md`:
```markdown
# 中文测试文档

搜索引擎是现代信息检索的核心技术。它支持关键词搜索和语义检索。

## 混合内容

Go语言实现的本地搜索引擎，支持中文和英文混合检索。
```

- [ ] **Step 2: Write failing tests for indexer**

Create `internal/service/indexer_test.go`:
```go
package service

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

func TestIndexCollection(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	if err := store.AddCollection(db, "test", dir, "*.md", nil); err != nil {
		t.Fatal(err)
	}

	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(db, tok)

	result, err := idx.UpdateCollection("test", dir, "*.md", nil, nil)
	if err != nil {
		t.Fatalf("UpdateCollection failed: %v", err)
	}

	if result.Indexed != 2 {
		t.Fatalf("expected 2 indexed, got %d", result.Indexed)
	}

	docs, _ := store.ListDocumentsByCollection(db, "test")
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents in db, got %d", len(docs))
	}
}

func TestIndexCollectionIncremental(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(db, tok)

	result1, _ := idx.UpdateCollection("test", dir, "*.md", nil, nil)
	if result1.Indexed != 2 {
		t.Fatalf("first run: expected 2 indexed, got %d", result1.Indexed)
	}

	result2, _ := idx.UpdateCollection("test", dir, "*.md", nil, nil)
	if result2.Unchanged != 2 {
		t.Fatalf("second run: expected 2 unchanged, got indexed=%d updated=%d unchanged=%d",
			result2.Indexed, result2.Updated, result2.Unchanged)
	}
}

func TestIndexCollectionDetectDeletion(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(db, tok)

	_, _ = idx.UpdateCollection("test", dir, "*.md", nil, nil)

	os.Remove(filepath.Join(dir, "chinese.md"))

	result, _ := idx.UpdateCollection("test", dir, "*.md", nil, nil)
	if result.Removed != 1 {
		t.Fatalf("expected 1 removed, got %d", result.Removed)
	}

	docs, _ := store.ListDocumentsByCollection(db, "test")
	if len(docs) != 1 {
		t.Fatalf("expected 1 remaining document, got %d", len(docs))
	}
}

func setupIndexTest(t *testing.T) (*sql.DB, string) {
	t.Helper()

	fixtureDir := filepath.Join("..", "..", "test", "fixtures")
	abs, err := filepath.Abs(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	files, _ := os.ReadDir(abs)
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(abs, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, f.Name()), data, 0644)
	}

	tdb := openTestServiceDB(t)
	return tdb, dir
}

func openTestServiceDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := store.OpenDB(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(db); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/service/ -run TestIndex -v`
Expected: FAIL — `NewIndexer` undefined

- [ ] **Step 4: Implement Indexer service**

Create `internal/service/indexer.go`:
```go
package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

type UpdateResult struct {
	Indexed   int
	Updated   int
	Unchanged int
	Removed   int
}

type Indexer struct {
	db       *sql.DB
	tokenizer tokenizer.Tokenizer
}

func NewIndexer(db *sql.DB, tok tokenizer.Tokenizer) *Indexer {
	return &Indexer{db: db, tokenizer: tok}
}

func (idx *Indexer) UpdateCollection(collectionName, rootDir, globPattern string, ignorePatterns []string, onProgress func(string)) (*UpdateResult, error) {
	result := &UpdateResult{}

	pattern := globPattern
	if pattern == "" {
		pattern = "**/*.md"
	}

	existingDocs, err := store.ListDocumentsByCollection(idx.db, collectionName)
	if err != nil {
		return nil, err
	}
	existingPaths := make(map[string]string)
	for _, d := range existingDocs {
		existingPaths[d.Path] = d.Hash
	}

	foundPaths := make(map[string]bool)

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		matched, _ := filepath.Match(filepath.Base(pattern), filepath.Base(relPath))
		if strings.Contains(pattern, "**/") {
			matched, _ = filepath.Match(strings.TrimPrefix(pattern, "**/"), filepath.Base(relPath))
		}
		if !matched {
			return nil
		}

		if onProgress != nil {
			onProgress(relPath)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		hash := hashContent(content)
		relPath = filepath.ToSlash(relPath)
		foundPaths[relPath] = true

		if existingHash, exists := existingPaths[relPath]; exists {
			if existingHash == hash {
				result.Unchanged++
				return nil
			}
			result.Updated++
		} else {
			result.Indexed++
		}

		title := extractTitle(string(content), relPath)
		body := string(content)
		tokenizedBody := idx.tokenizer.TokenizeToString(body)
		tokenizedTitle := idx.tokenizer.TokenizeToString(title)

		doc := &store.DocumentRecord{
			Collection: collectionName,
			Path:       relPath,
			Title:      title,
			Body:       body,
			Hash:       hash,
			FileSize:   int64(len(content)),
		}

		return store.UpsertDocument(idx.db, doc, tokenizedBody, tokenizedTitle)
	})
	if err != nil {
		return nil, err
	}

	for path := range existingPaths {
		if !foundPaths[path] {
			doc, err := store.GetDocumentByPath(idx.db, collectionName, path)
			if err == nil {
				store.DeleteDocument(idx.db, doc.ID)
				result.Removed++
			}
		}
	}

	return result, nil
}

func hashContent(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

var headingRe = regexp.MustCompile(`^#\s+(.+)$`)

func extractTitle(content, fallback string) string {
	lines := strings.SplitN(content, "\n", 20)
	for _, line := range lines {
		m := headingRe.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return strings.TrimSuffix(filepath.Base(fallback), filepath.Ext(fallback))
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/service/ -run TestIndex -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add indexer service with incremental update support"
```

### Task 8: Searcher service (BM25)

**Files:**
- Create: `internal/service/searcher.go`
- Test: `internal/service/searcher_test.go`

- [ ] **Step 1: Write failing tests for BM25 search**

Create `internal/service/searcher_test.go`:
```go
package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

func TestSearchBM25(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil, nil)

	searcher := NewSearcher(db, tok)

	results, err := searcher.SearchLex("搜索引擎", "", 10, 0)
	if err != nil {
		t.Fatalf("SearchLex failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for '搜索引擎'")
	}

	found := false
	for _, r := range results {
		if r.Collection == "test" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected results from test collection")
	}
}

func TestSearchBM25WithCollection(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil, nil)

	searcher := NewSearcher(db, tok)

	results, err := searcher.SearchLex("搜索引擎", "nonexistent", 10, 0)
	if err != nil {
		t.Fatalf("SearchLex failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatal("expected no results from nonexistent collection")
	}
}

func TestSearchBM25English(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil, nil)

	searcher := NewSearcher(db, tok)

	results, err := searcher.SearchLex("Hello", "", 10, 0)
	if err != nil {
		t.Fatalf("SearchLex failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'Hello'")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/ -run TestSearchBM25 -v`
Expected: FAIL — `NewSearcher` undefined

- [ ] **Step 3: Implement Searcher service**

Create `internal/service/searcher.go`:
```go
package service

import (
	"database/sql"
	"strings"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

type SearchHit struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
}

type Searcher struct {
	db        *sql.DB
	tokenizer tokenizer.Tokenizer
}

func NewSearcher(db *sql.DB, tok tokenizer.Tokenizer) *Searcher {
	return &Searcher{db: db, tokenizer: tok}
}

func (s *Searcher) SearchLex(query, collection string, limit int, minScore float64) ([]SearchHit, error) {
	var tokenized string
	if s.tokenizer != nil {
		tokenized = s.tokenizer.TokenizeToString(query)
	} else {
		tokenized = query
	}

	if tokenized == "" {
		return nil, nil
	}

	ftsResults, err := store.SearchFTS(s.db, tokenized, collection, limit)
	if err != nil {
		return nil, err
	}

	var hits []SearchHit
	for _, r := range ftsResults {
		if r.Score < minScore {
			continue
		}

		doc, err := store.GetDocumentByDocID(s.db, r.DocID)
		if err != nil {
			continue
		}

		snippet := extractSnippet(doc.Body, query, 200)
		line := findLineNumber(doc.Body, query)

		hits = append(hits, SearchHit{
			DocID:      r.DocID,
			Collection: r.Collection,
			Path:       r.Path,
			Title:      r.Title,
			Score:      r.Score,
			Snippet:    snippet,
			Line:       line,
		})
	}

	return hits, nil
}

func extractSnippet(body, query string, maxLen int) string {
	idx := strings.Index(strings.ToLower(body), strings.ToLower(query))
	if idx == -1 {
		if len(body) > maxLen {
			return body[:maxLen] + "..."
		}
		return body
	}

	start := idx - maxLen/3
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(body) {
		end = len(body)
	}

	snippet := body[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(body) {
		snippet = snippet + "..."
	}
	return snippet
}

func findLineNumber(body, query string) int {
	idx := strings.Index(strings.ToLower(body), strings.ToLower(query))
	if idx == -1 {
		return 1
	}
	return strings.Count(body[:idx], "\n") + 1
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/service/ -run TestSearchBM25 -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat: add BM25 searcher service"
```

---

## Chunk 7: CLI Commands

### Task 9: Collection CLI commands

**Files:**
- Create: `internal/cli/collection.go`

- [ ] **Step 1: Implement collection CLI commands**

Create `internal/cli/collection.go`:
```go
package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/spf13/cobra"
)

var (
	collectionName string
	collectionMask string
)

var collectionCmd = &cobra.Command{
	Use:   "collection",
	Short: "Manage collections",
}

var collectionAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a collection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if collectionName == "" {
			return fmt.Errorf("--name is required")
		}

		absPath, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}

		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			return fmt.Errorf("path does not exist: %s", absPath)
		}

		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		mask := collectionMask
		if mask == "" {
			mask = "**/*.md"
		}

		if err := store.AddCollection(db, collectionName, absPath, mask, nil); err != nil {
			return err
		}

		fmt.Printf("Collection '%s' added: %s\n", collectionName, absPath)
		return nil
	},
}

var collectionRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a collection",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		if err := store.RemoveCollection(db, args[0]); err != nil {
			return err
		}

		fmt.Printf("Collection '%s' removed\n", args[0])
		return nil
	},
}

var collectionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all collections",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		cols, err := store.ListCollections(db)
		if err != nil {
			return err
		}

		if len(cols) == 0 {
			fmt.Println("No collections found.")
			return nil
		}

		for _, c := range cols {
			fmt.Printf("%s\t%s\t(%d docs)\t%s\n", c.Name, c.Path, c.DocCount, c.GlobPattern)
		}
		return nil
	},
}

var collectionRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename a collection",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		if err := store.RenameCollection(db, args[0], args[1]); err != nil {
			return err
		}

		fmt.Printf("Collection renamed: %s -> %s\n", args[0], args[1])
		return nil
	},
}

func init() {
	collectionAddCmd.Flags().StringVar(&collectionName, "name", "", "collection name (required)")
	collectionAddCmd.Flags().StringVar(&collectionMask, "mask", "**/*.md", "file glob pattern")

	collectionCmd.AddCommand(collectionAddCmd)
	collectionCmd.AddCommand(collectionRemoveCmd)
	collectionCmd.AddCommand(collectionListCmd)
	collectionCmd.AddCommand(collectionRenameCmd)
	rootCmd.AddCommand(collectionCmd)
}
```

- [ ] **Step 2: Verify build**

Run: `go build ./cmd/lmd/`
Expected: builds without errors

- [ ] **Step 3: Manual smoke test**

```bash
./lmd collection add /tmp --name test
./lmd collection list
./lmd collection remove test
```

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat: add collection CLI commands (add/remove/list/rename)"
```

### Task 10: Update, Search, Get, Status CLI commands

**Files:**
- Create: `internal/cli/index.go`
- Create: `internal/cli/search.go`
- Create: `internal/cli/get.go`

- [ ] **Step 1: Implement update command**

Create `internal/cli/index.go`:
```go
package cli

import (
	"fmt"

	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/spf13/cobra"
)

var updateCollection string

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Scan filesystem and update index",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		idx := service.NewIndexer(db, tok)

		cols, err := store.ListCollections(db)
		if err != nil {
			return err
		}

		totalIndexed := 0
		totalUpdated := 0
		totalUnchanged := 0
		totalRemoved := 0

		for _, col := range cols {
			if updateCollection != "" && col.Name != updateCollection {
				continue
			}

			result, err := idx.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns, func(f string) {
				if verbose {
					fmt.Printf("  indexing: %s\n", f)
				}
			})
			if err != nil {
				fmt.Printf("Error indexing %s: %v\n", col.Name, err)
				continue
			}

			fmt.Printf("%s: indexed=%d updated=%d unchanged=%d removed=%d\n",
				col.Name, result.Indexed, result.Updated, result.Unchanged, result.Removed)
			totalIndexed += result.Indexed
			totalUpdated += result.Updated
			totalUnchanged += result.Unchanged
			totalRemoved += result.Removed
		}

		fmt.Printf("\nTotal: indexed=%d updated=%d unchanged=%d removed=%d\n",
			totalIndexed, totalUpdated, totalUnchanged, totalRemoved)
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show index status",
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		cols, err := store.ListCollections(db)
		if err != nil {
			return err
		}

		fmt.Printf("Database: %s\n\n", getDefaultIndexPath())
		if len(cols) == 0 {
			fmt.Println("No collections.")
			return nil
		}

		for _, c := range cols {
			fmt.Printf("  %s\n", c.Name)
			fmt.Printf("    Path:  %s\n", c.Path)
			fmt.Printf("    Glob:  %s\n", c.GlobPattern)
			fmt.Printf("    Docs:  %d\n", c.DocCount)
		}
		return nil
	},
}

func init() {
	updateCmd.Flags().StringVarP(&updateCollection, "collection", "c", "", "update specific collection only")
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(statusCmd)
}
```

- [ ] **Step 2: Implement search command**

Create `internal/cli/search.go`:
```go
package cli

import (
	"fmt"

	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/spf13/cobra"
)

var (
	searchCollection string
	searchLimit      int
	searchFull       bool
	searchMinScore   float64
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "BM25 keyword search",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		tok, err := tokenizer.NewGseTokenizer()
		if err != nil {
			return fmt.Errorf("failed to initialize tokenizer: %w", err)
		}

		searcher := service.NewSearcher(db, tok)
		results, err := searcher.SearchLex(args[0], searchCollection, searchLimit, searchMinScore)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			fmt.Println("No results found.")
			return nil
		}

		for _, r := range results {
			fmt.Printf("%s:%d #%s\n", r.Path, r.Line, r.DocID)
			fmt.Printf("Title: %s\n", r.Title)
			fmt.Printf("Score: %.0f%%\n", r.Score*100)
			if searchFull {
				fmt.Println()
				doc, err := store.GetDocumentByDocID(db, r.DocID)
				if err == nil {
					fmt.Println(doc.Body)
				} else {
					fmt.Println(r.Snippet)
				}
			} else {
				fmt.Printf("\n%s\n", r.Snippet)
			}
			fmt.Println()
		}
		return nil
	},
}

func init() {
	searchCmd.Flags().StringVarP(&searchCollection, "collection", "c", "", "search in specific collection")
	searchCmd.Flags().IntVarP(&searchLimit, "limit", "n", 5, "number of results")
	searchCmd.Flags().BoolVar(&searchFull, "full", false, "show full document content")
	searchCmd.Flags().Float64Var(&searchMinScore, "min-score", 0, "minimum score threshold")
	rootCmd.AddCommand(searchCmd)
}
```

- [ ] **Step 3: Implement get command**

Create `internal/cli/get.go`:
```go
package cli

import (
	"fmt"
	"strings"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/spf13/cobra"
)

var (
	getFull bool
	getFrom int
	getLines int
)

var getCmd = &cobra.Command{
	Use:   "get <path-or-docid>",
	Short: "Get a document by path or docid",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		input := args[0]
		var doc *store.DocumentRecord

		if strings.HasPrefix(input, "#") {
			doc, err = store.GetDocumentByDocID(db, input[1:])
		} else {
			parts := strings.SplitN(input, "/", 2)
			if len(parts) == 2 {
				doc, err = store.GetDocumentByPath(db, parts[0], parts[1])
			} else {
				return fmt.Errorf("invalid path format, use collection/path or #docid: %s", input)
			}
		}

		if err != nil {
			return fmt.Errorf("document not found: %s", input)
		}

		fmt.Printf("#%s %s\n", doc.DocID, doc.Title)
		fmt.Printf("Collection: %s\n", doc.Collection)
		fmt.Printf("Path: %s\n", doc.Path)
		fmt.Printf("Size: %d bytes\n", doc.FileSize)
		fmt.Println()

		body := doc.Body
		if !getFull {
			if len(body) > 500 {
				body = body[:500] + "..."
			}
		}
		if getFrom > 0 {
			lines := strings.Split(body, "\n")
			if getFrom <= len(lines) {
				body = strings.Join(lines[getFrom-1:], "\n")
			}
		}
		if getLines > 0 {
			lines := strings.Split(body, "\n")
			if getLines < len(lines) {
				body = strings.Join(lines[:getLines], "\n")
			}
		}

		fmt.Println(body)
		return nil
	},
}

func init() {
	getCmd.Flags().BoolVar(&getFull, "full", false, "show full document")
	getCmd.Flags().IntVar(&getFrom, "from", 0, "start from line number")
	getCmd.Flags().IntVarP(&getLines, "lines", "l", 0, "max lines to show")
	rootCmd.AddCommand(getCmd)
}
```

- [ ] **Step 4: Verify build**

Run: `go build ./cmd/lmd/`
Expected: builds without errors

- [ ] **Step 5: Manual end-to-end test**

```bash
mkdir -p /tmp/lmd-test
cat > /tmp/lmd-test/test.md << 'EOF'
# 测试文档

这是一个搜索引擎的测试文档。支持中文和英文混合检索。
EOF

./lmd collection add /tmp/lmd-test --name test
./lmd update
./lmd search "搜索引擎"
./lmd get "test/test.md"
./lmd status
```

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add update, search, get, and status CLI commands"
```

---

## Chunk 8: Public API & Integration Test

### Task 11: Public API (pkg/)

**Files:**
- Create: `pkg/lmd.go`
- Test: `pkg/lmd_test.go`

- [ ] **Step 1: Write failing integration test**

Create `pkg/lmd_test.go`:
```go
package lmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	store, err := CreateStore(StoreOptions{
		DBPath: dbPath,
	})
	if err != nil {
		t.Fatalf("CreateStore failed: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestStoreCollectionWorkflow(t *testing.T) {
	dir := t.TempDir()
	testDir := filepath.Join(dir, "docs")
	os.MkdirAll(testDir, 0755)

	os.WriteFile(filepath.Join(testDir, "test.md"), []byte("# Hello\n\nWorld test content"), 0644)

	store, err := CreateStore(StoreOptions{
		DBPath: filepath.Join(dir, "test.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	err = store.AddCollection("docs", CollectionConfig{
		Path:        testDir,
		GlobPattern: "*.md",
	})
	if err != nil {
		t.Fatal(err)
	}

	cols, err := store.ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].Name != "docs" {
		t.Fatalf("unexpected collections: %v", cols)
	}

	result, err := store.Update(context.Background(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Indexed != 1 {
		t.Fatalf("expected 1 indexed, got %d", result.Indexed)
	}

	results, err := store.SearchLex("Hello", LexOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}

	doc, err := store.Get(results[0].Collection + "/" + results[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Hello" {
		t.Fatalf("expected title 'Hello', got '%s'", doc.Title)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/ -v`
Expected: FAIL — package doesn't exist

- [ ] **Step 3: Implement public API**

Create `pkg/lmd.go`:
```go
package lmd

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

type CollectionConfig struct {
	Path           string
	GlobPattern    string
	IgnorePatterns []string
}

type CollectionInfo struct {
	Name        string
	Path        string
	GlobPattern string
	DocCount    int
}

type StoreOptions struct {
	DBPath string
}

type UpdateOptions struct {
	Collections []string
}

type UpdateResult = service.UpdateResult

type LexOptions struct {
	Collection string
	Limit      int
	MinScore   float64
}

type SearchResult struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
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

type lmdStore struct {
	db        *sql.DB
	tokenizer tokenizer.Tokenizer
	indexer   *service.Indexer
	searcher  *service.Searcher
}

func CreateStore(opts StoreOptions) (*lmdStore, error) {
	db, err := store.OpenAndMigrate(opts.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize tokenizer: %w", err)
	}

	return &lmdStore{
		db:        db,
		tokenizer: tok,
		indexer:   service.NewIndexer(db, tok),
		searcher:  service.NewSearcher(db, tok),
	}, nil
}

func (s *lmdStore) AddCollection(name string, config CollectionConfig) error {
	glob := config.GlobPattern
	if glob == "" {
		glob = "**/*.md"
	}
	return store.AddCollection(s.db, name, config.Path, glob, config.IgnorePatterns)
}

func (s *lmdStore) RemoveCollection(name string) error {
	return store.RemoveCollection(s.db, name)
}

func (s *lmdStore) ListCollections() ([]CollectionInfo, error) {
	cols, err := store.ListCollections(s.db)
	if err != nil {
		return nil, err
	}
	result := make([]CollectionInfo, len(cols))
	for i, c := range cols {
		result[i] = CollectionInfo{
			Name:        c.Name,
			Path:        c.Path,
			GlobPattern: c.GlobPattern,
			DocCount:    c.DocCount,
		}
	}
	return result, nil
}

func (s *lmdStore) Update(ctx context.Context, opts UpdateOptions) (*UpdateResult, error) {
	cols, err := store.ListCollections(s.db)
	if err != nil {
		return nil, err
	}

	total := &UpdateResult{}
	for _, col := range cols {
		if len(opts.Collections) > 0 {
			found := false
			for _, name := range opts.Collections {
				if name == col.Name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		result, err := s.indexer.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns, nil)
		if err != nil {
			return nil, err
		}
		total.Indexed += result.Indexed
		total.Updated += result.Updated
		total.Unchanged += result.Unchanged
		total.Removed += result.Removed
	}
	return total, nil
}

func (s *lmdStore) SearchLex(query string, opts LexOptions) ([]SearchResult, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 10
	}
	hits, err := s.searcher.SearchLex(query, opts.Collection, limit, opts.MinScore)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, len(hits))
	for i, h := range hits {
		results[i] = SearchResult{
			DocID:      h.DocID,
			Collection: h.Collection,
			Path:       h.Path,
			Title:      h.Title,
			Score:      h.Score,
			Snippet:    h.Snippet,
			Line:       h.Line,
		}
	}
	return results, nil
}

func (s *lmdStore) Get(pathOrDocID string) (*Document, error) {
	var doc *store.DocumentRecord
	var err error

	if len(pathOrDocID) > 0 && pathOrDocID[0] == '#' {
		doc, err = store.GetDocumentByDocID(s.db, pathOrDocID[1:])
	} else {
		parts := splitPath(pathOrDocID)
		if len(parts) == 2 {
			doc, err = store.GetDocumentByPath(s.db, parts[0], parts[1])
		} else {
			return nil, fmt.Errorf("invalid path format, use collection/path or #docid")
		}
	}
	if err != nil {
		return nil, err
	}

	return &Document{
		DocID:      doc.DocID,
		Collection: doc.Collection,
		Path:       doc.Path,
		Title:      doc.Title,
		Body:       doc.Body,
		FileSize:   doc.FileSize,
		ModifiedAt: doc.ModifiedAt,
	}, nil
}

func (s *lmdStore) Close() error {
	return s.db.Close()
}

func splitPath(p string) []string {
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return []string{p[:i], p[i+1:]}
		}
	}
	return []string{p}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./pkg/ -v`
Expected: PASS

- [ ] **Step 5: Run all tests**

Run: `go test ./... -v`
Expected: all PASS

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "feat: add public API (pkg/) with integration tests"
```

---

## Chunk 9: Test Fixtures & Final Verification

### Task 12: Add comprehensive test fixtures

**Files:**
- Create: `test/fixtures/simple.md`
- Create: `test/fixtures/chinese.md`

- [ ] **Step 1: Create test fixtures**

These should have been created in Task 7. Verify they exist. If not, create them.

`test/fixtures/simple.md`:
```markdown
# Simple Test Document

这是一个简单的测试文档。

## 第二段

Hello World, mixed content here.
```

`test/fixtures/chinese.md`:
```markdown
# 中文搜索引擎测试

搜索引擎是现代信息检索的核心技术。它支持关键词搜索和语义向量检索。

## 混合内容

Go语言实现的本地搜索引擎，专门支持中文和英文混合检索场景。
支持个人知识库管理和Agent记忆层功能。
```

- [ ] **Step 2: Run full test suite**

Run: `go test ./... -v -count=1`
Expected: all PASS

- [ ] **Step 3: Run vet and build**

Run:
```bash
go vet ./...
go build ./cmd/lmd/
```
Expected: no errors

- [ ] **Step 4: End-to-end CLI test**

```bash
rm -rf /tmp/lmd-e2e
mkdir -p /tmp/lmd-e2e/notes

cat > /tmp/lmd-e2e/notes/go.md << 'EOF'
# Go语言笔记

Go是一门编译型语言，支持并发编程。goroutine和channel是核心特性。

## 并发模式

使用goroutine实现轻量级并发，channel实现通信。
EOF

cat > /tmp/lmd-e2e/notes/python.md << 'EOF'
# Python笔记

Python是解释型语言，适合数据科学和机器学习。

## 数据处理

pandas和numpy是常用的数据处理库。
EOF

./lmd --index /tmp/lmd-e2e/test.sqlite collection add /tmp/lmd-e2e/notes --name notes
./lmd --index /tmp/lmd-e2e/test.sqlite update --collection notes
./lmd --index /tmp/lmd-e2e/test.sqlite search "并发编程"
./lmd --index /tmp/lmd-e2e/test.sqlite search "数据科学"
./lmd --index /tmp/lmd-e2e/test.sqlite get "notes/go.md"
./lmd --index /tmp/lmd-e2e/test.sqlite status
./lmd --index /tmp/lmd-e2e/test.sqlite collection list
```

Expected: search returns correct Chinese results, get shows document, status shows collection info.

- [ ] **Step 5: Final commit**

```bash
git add -A
git commit -m "feat: complete Phase 1 foundation with test fixtures and e2e verification"
```

---

## Summary

Phase 1 produces a working CLI tool that can:
- Manage collections (add/remove/list/rename)
- Index Markdown files with Chinese-aware tokenization (gse + FTS5)
- Search with BM25 keyword search
- Retrieve documents by path or docid
- Show index status

**9 files created** in `internal/`, **1 file** in `pkg/`, **1 file** in `cmd/`, **2 test fixtures**.

**Next phases** (separate plans):
- Phase 2: Vector embedding + sqlite-vec + vsearch
- Phase 3: RRF fusion + reranker + query expansion
- Phase 4: MCP server + context system + formatters
