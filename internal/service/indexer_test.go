package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

func TestIndexCollection(t *testing.T) {
	db, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	if err := dao.AddCollection("test", dir, "*.md", nil); err != nil {
		t.Fatal(err)
	}

	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	result, err := idx.UpdateCollection("test", dir, "*.md", nil)
	if err != nil {
		t.Fatalf("UpdateCollection failed: %v", err)
	}

	if result.Indexed != 2 {
		t.Fatalf("expected 2 indexed, got indexed=%d updated=%d unchanged=%d",
			result.Indexed, result.Updated, result.Unchanged)
	}

	docs, _ := dao.ListDocumentsByCollection("test")
	if len(docs) != 2 {
		t.Fatalf("expected 2 documents in db, got %d", len(docs))
	}

	_ = db
}

func TestIndexCollectionIncremental(t *testing.T) {
	db, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	result1, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result1.Indexed != 2 {
		t.Fatalf("first run: expected 2 indexed, got %d", result1.Indexed)
	}

	result2, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result2.Unchanged != 2 {
		t.Fatalf("second run: expected 2 unchanged, got indexed=%d updated=%d unchanged=%d",
			result2.Indexed, result2.Updated, result2.Unchanged)
	}

	_ = db
}

func TestIndexCollectionDetectDeletion(t *testing.T) {
	db, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	os.Remove(filepath.Join(dir, "chinese.md"))

	result, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result.Removed != 1 {
		t.Fatalf("expected 1 removed, got %d", result.Removed)
	}

	docs, _ := dao.ListDocumentsByCollection("test")
	if len(docs) != 1 {
		t.Fatalf("expected 1 remaining document, got %d", len(docs))
	}

	_ = db
}

func TestTimestampUnchangedFilesSkipped(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	result1, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result1.Indexed != 2 {
		t.Fatalf("first run: expected 2 indexed, got %d", result1.Indexed)
	}

	result2, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result2.Unchanged != 2 {
		t.Fatalf("second run: expected 2 unchanged via timestamp, got indexed=%d updated=%d unchanged=%d",
			result2.Indexed, result2.Updated, result2.Unchanged)
	}
}

func TestTimestampChangedFilesReindexed(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	result1, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result1.Indexed != 2 {
		t.Fatalf("first run: expected 2 indexed, got %d", result1.Indexed)
	}

	target := filepath.Join(dir, "simple.md")
	orig, _ := os.ReadFile(target)
	time.Sleep(10 * time.Millisecond)
	os.WriteFile(target, append(orig, []byte("\n\n# Updated content")...), 0644)

	result2, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result2.Updated != 1 {
		t.Fatalf("expected 1 updated after modification, got indexed=%d updated=%d unchanged=%d",
			result2.Indexed, result2.Updated, result2.Unchanged)
	}
}

func TestNewFilesIndexed(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	result1, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result1.Indexed != 2 {
		t.Fatalf("first run: expected 2 indexed, got %d", result1.Indexed)
	}

	os.WriteFile(filepath.Join(dir, "newfile.md"), []byte("# New File\n\nNew content."), 0644)

	result2, _ := idx.UpdateCollection("test", dir, "*.md", nil)
	if result2.Indexed != 1 {
		t.Fatalf("expected 1 new indexed, got indexed=%d updated=%d unchanged=%d",
			result2.Indexed, result2.Updated, result2.Unchanged)
	}
}

func setupIndexTest(t *testing.T) (*dao.Store, string, func()) {
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

	cleanup := openTestServiceDB(t)
	return dao.DB, dir, cleanup
}

func openTestServiceDB(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	if err := dao.Init(filepath.Join(dir, "test.sqlite")); err != nil {
		t.Fatal(err)
	}
	return func() {
		if dao.DB != nil {
			dao.DB.Close()
		}
	}
}
