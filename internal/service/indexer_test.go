package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lixianmin/lmd/internal/chunker"
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
			dao.DB = nil
		}
	}
}

func TestExpandGlobPattern(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"**/*.md", []string{"**/*.md"}},
		{"**/*.{md,txt}", []string{"**/*.md", "**/*.txt"}},
		{"*.{md,txt,html}", []string{"*.md", "*.txt", "*.html"}},
		{"*.md", []string{"*.md"}},
		{"**/*", []string{"**/*"}},
	}
	for _, tt := range tests {
		result := expandGlobPattern(tt.input)
		if len(result) != len(tt.expected) {
			t.Fatalf("expandGlobPattern(%q): expected %v, got %v", tt.input, tt.expected, result)
		}
		for i, p := range result {
			if p != tt.expected[i] {
				t.Fatalf("expandGlobPattern(%q)[%d]: expected %q, got %q", tt.input, i, tt.expected[i], p)
			}
		}
	}
}

func TestIndexTXTFiles(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	txtContent := ""
	for i := 0; i < 50; i++ {
		txtContent += "This is line " + string(rune('0'+i%10)) + " of the text file.\n"
	}
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte(txtContent), 0644)

	if err := dao.AddCollection("test", dir, "*.{md,txt}", nil); err != nil {
		t.Fatal(err)
	}

	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	result, err := idx.UpdateCollection("test", dir, "*.{md,txt}", nil)
	if err != nil {
		t.Fatalf("UpdateCollection failed: %v", err)
	}

	if result.Indexed != 3 {
		t.Fatalf("expected 3 indexed (2 md + 1 txt), got indexed=%d", result.Indexed)
	}

	docs, _ := dao.ListDocumentsByCollection("test")
	if len(docs) != 3 {
		t.Fatalf("expected 3 documents in db, got %d", len(docs))
	}
}

func TestIndexerSelectsChunkerByExt(t *testing.T) {
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	mdChunker := idx.chunkerForExt(".md")
	if _, ok := mdChunker.(*chunker.MarkdownChunker); !ok {
		t.Fatal("expected MarkdownChunker for .md")
	}

	txtChunker := idx.chunkerForExt(".txt")
	if _, ok := txtChunker.(*chunker.PlainTextChunker); !ok {
		t.Fatal("expected PlainTextChunker for .txt")
	}

	defaultChunker := idx.chunkerForExt(".html")
	if _, ok := defaultChunker.(*chunker.MarkdownChunker); !ok {
		t.Fatal("expected MarkdownChunker for unknown ext")
	}
}

func TestIgnorePatternsExcludeFiles(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	os.WriteFile(filepath.Join(dir, "important.md"), []byte("# Important\n\nThis should be indexed."), 0644)
	os.WriteFile(filepath.Join(dir, "draft.tmp"), []byte("# Draft\n\nThis is a temp file."), 0644)
	os.WriteFile(filepath.Join(dir, "notes.log"), []byte("log file content"), 0644)

	os.MkdirAll(filepath.Join(dir, ".git"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("git config"), 0644)

	_ = dao.AddCollection("test", dir, "*.md", []string{"*.tmp", "*.log", ".git"})
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	result, err := idx.UpdateCollection("test", dir, "*.md", []string{"*.tmp", "*.log", ".git"})
	if err != nil {
		t.Fatalf("UpdateCollection failed: %v", err)
	}

	docs, _ := dao.ListDocumentsByCollection("test")
	for _, d := range docs {
		if filepath.Ext(d.Path) == ".tmp" {
			t.Fatalf("ignore pattern '*.tmp' should exclude %s", d.Path)
		}
		if filepath.Ext(d.Path) == ".log" {
			t.Fatalf("ignore pattern '*.log' should exclude %s", d.Path)
		}
		if strings.HasPrefix(d.Path, ".git") {
			t.Fatalf("ignore pattern '.git' should exclude %s", d.Path)
		}
	}

	if result.Indexed < 1 {
		t.Fatalf("expected at least 1 .md file indexed, got indexed=%d", result.Indexed)
	}
}

func TestScanChangesNewFiles(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	dao.AddCollection("notes", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	pending, err := idx.ScanChanges("notes", dir, "*.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending docs, got %d", len(pending))
	}
	for _, p := range pending {
		if p.Action != DocNew {
			t.Fatalf("expected DocNew, got %d", p.Action)
		}
		if len(p.Chunks) == 0 {
			t.Fatal("expected chunks to be populated for", p.Path)
		}
		if p.Body == "" {
			t.Fatal("expected body to be populated for", p.Path)
		}
	}

	docs, _ := dao.ListDocumentsByCollection("notes")
	if len(docs) != 0 {
		t.Fatalf("ScanChanges should NOT write to DB, found %d docs", len(docs))
	}
}

func TestScanChangesDetectDeletion(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	dao.AddCollection("notes", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	idx.UpdateCollection("notes", dir, "*.md", nil)

	os.Remove(filepath.Join(dir, "chinese.md"))

	pending, err := idx.ScanChanges("notes", dir, "*.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending (deleted), got %d", len(pending))
	}
	if pending[0].Action != DocDeleted {
		t.Fatalf("expected DocDeleted, got %d", pending[0].Action)
	}
	if pending[0].OldDocId == 0 {
		t.Fatal("expected OldDocId to be set for deletion")
	}
}

func TestScanChangesUnchanged(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	dao.AddCollection("notes", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	idx.UpdateCollection("notes", dir, "*.md", nil)

	pending, err := idx.ScanChanges("notes", dir, "*.md", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending (unchanged), got %d", len(pending))
	}
}

func TestScanChangesIncompleteDoc(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	dao.AddCollection("notes", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)

	dao.InsertDocument("notes", "chinese.md", "Chinese Title", "content", 7, "somehash")

	pending, err := idx.ScanChanges("notes", dir, "*.md", nil)
	if err != nil {
		t.Fatal(err)
	}

	var incompletePending *PendingDoc
	for i := range pending {
		if pending[i].Path == "chinese.md" {
			incompletePending = &pending[i]
			break
		}
	}
	if incompletePending == nil {
		t.Fatal("expected a pending doc for chinese.md (incomplete)")
	}
	if incompletePending.Action != DocChanged {
		t.Fatalf("expected DocChanged for incomplete doc, got %d", incompletePending.Action)
	}
}
