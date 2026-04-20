package service

import (
	"context"
	"testing"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

func TestEmbedChunks(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(provider, 8, 800)

	result, err := embedder.EmbedBatch(context.Background(), 0)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if result.Embedded == 0 {
		t.Fatal("expected some chunks to be embedded")
	}
}

func TestEmbedChunksIdempotent(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(provider, 8, 800)

	embedder.EmbedBatch(context.Background(), 0)
	r2, _ := embedder.EmbedBatch(context.Background(), 0)
	if r2.Embedded != 0 {
		t.Fatalf("second run should embed 0 (all done), got %d", r2.Embedded)
	}
	if r2.Skipped != 0 {
		t.Fatalf("expected 0 skipped, got %d", r2.Skipped)
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
