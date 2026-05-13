package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/lixianmin/lmd/internal/chunker"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

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

func TestChunkIndexerSelectsChunkerByExt(t *testing.T) {
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewChunkIndexer(tok, embedding.NewMockProvider(dao.EmbeddingDim))

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

func indexHelper(t *testing.T, idx *ChunkIndexer, name, dir, pattern string, ignores []string) {
	t.Helper()
	pending, err := idx.ScanChanges(name, dir, pattern, ignores)
	if err != nil {
		t.Fatalf("ScanChanges failed: %v", err)
	}
	for _, doc := range pending {
		if err := idx.ProcessDoc(context.Background(), doc); err != nil {
			t.Fatalf("ProcessDoc failed: %v", err)
		}
	}
}
