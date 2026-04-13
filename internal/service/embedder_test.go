package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

func TestEmbedChunks(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(db, provider)

	result, err := embedder.EmbedAll("mock", false)
	if err != nil {
		t.Fatalf("EmbedAll failed: %v", err)
	}
	if result.Embedded == 0 {
		t.Fatal("expected some chunks to be embedded")
	}
}

func TestEmbedChunksIdempotent(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(db, provider)

	r1, _ := embedder.EmbedAll("mock", false)
	r2, _ := embedder.EmbedAll("mock", false)
	if r2.Embedded != 0 {
		t.Fatalf("second run should embed 0 (all done), got %d", r2.Embedded)
	}
	if r2.Skipped != r1.Embedded {
		t.Fatalf("expected %d skipped, got %d", r1.Embedded, r2.Skipped)
	}
}

func newTestTokenizer(t *testing.T) *tokenizer.GseTokenizer {
	t.Helper()
	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		t.Fatal(err)
	}
	return tok
}
