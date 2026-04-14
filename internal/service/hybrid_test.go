package service

import (
	"context"
	"testing"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
)

func TestSearchHybrid(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(provider)
	_, _ = embedder.EmbedBatch(context.Background(), 0)

	searcher := NewSearcher(tok)
	results, err := searcher.SearchHybrid(provider, "并发编程", "", 5, 0)
	if err != nil {
		t.Fatalf("SearchHybrid failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected hybrid search results")
	}
}

func TestSearchHybridCollection(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(provider)
	_, _ = embedder.EmbedBatch(context.Background(), 0)

	searcher := NewSearcher(tok)
	results, err := searcher.SearchHybrid(provider, "并发编程", "test", 5, 0)
	if err != nil {
		t.Fatalf("SearchHybrid failed: %v", err)
	}
	for _, r := range results {
		if r.Collection != "test" {
			t.Fatalf("expected only 'test' collection, got %s", r.Collection)
		}
	}
}
