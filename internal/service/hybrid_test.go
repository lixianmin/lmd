package service

import (
	"context"
	"testing"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/store"
)

func TestSearchHybrid(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(db, provider)
	_, _ = embedder.EmbedAll(context.Background())

	searcher := NewSearcher(db, tok)
	results, err := searcher.SearchHybrid(provider, "并发编程", "", 5, 0)
	if err != nil {
		t.Fatalf("SearchHybrid failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected hybrid search results")
	}
}

func TestSearchHybridCollection(t *testing.T) {
	db, dir := setupIndexTest(t)
	defer db.Close()

	_ = store.AddCollection(db, "test", dir, "*.md", nil)
	tok := newTestTokenizer(t)
	idx := NewIndexer(db, tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	provider := embedding.NewMockProvider(1024)
	embedder := NewEmbedder(db, provider)
	_, _ = embedder.EmbedAll(context.Background())

	searcher := NewSearcher(db, tok)
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
