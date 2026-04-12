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

	result, err := idx.UpdateCollection("test", dir, "*.md", nil)
	if err != nil {
		t.Fatalf("UpdateCollection failed: %v", err)
	}

	if result.Indexed != 2 {
		t.Fatalf("expected 2 indexed, got indexed=%d updated=%d unchanged=%d",
			result.Indexed, result.Updated, result.Unchanged)
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

	result1, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result1.Indexed != 2 {
		t.Fatalf("first run: expected 2 indexed, got %d", result1.Indexed)
	}

	result2, _ := idx.UpdateCollection("test", dir, "*.md", nil)
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

	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	os.Remove(filepath.Join(dir, "chinese.md"))

	result, _ := idx.UpdateCollection("test", dir, "*.md", nil)
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
