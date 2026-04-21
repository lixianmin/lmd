package service

import (
	"context"
	"testing"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

func TestSearchBM25(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	searcher := NewSearcher(tok)

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
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	searcher := NewSearcher(tok)

	results, err := searcher.SearchLex("搜索引擎", "nonexistent", 10, 0)
	if err != nil {
		t.Fatalf("SearchLex failed: %v", err)
	}
	if len(results) != 0 {
		t.Fatal("expected no results from nonexistent collection")
	}
}

func TestSearchBM25English(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	searcher := NewSearcher(tok)

	results, err := searcher.SearchLex("Hello", "", 10, 0)
	if err != nil {
		t.Fatalf("SearchLex failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'Hello'")
	}
}

func TestApplyMMRFewerThanTopK(t *testing.T) {
	searcher := NewSearcher(nil)
	results := []formatter.SearchHit{
		{ChunkId: 1, Score: 0.9},
		{ChunkId: 2, Score: 0.8},
	}
	provider := embedding.NewMockProvider(16)

	got := searcher.ApplyMMR(results, provider, "test", 0.7, 5)
	if len(got) != 2 {
		t.Fatalf("expected 2 results (fewer than topK), got %d", len(got))
	}
}

func TestApplyMMREmptyResults(t *testing.T) {
	searcher := NewSearcher(nil)
	provider := embedding.NewMockProvider(16)

	got := searcher.ApplyMMR(nil, provider, "test", 0.7, 3)
	if len(got) != 0 {
		t.Fatalf("expected 0 results, got %d", len(got))
	}
}

func TestApplyMMRWithDB(t *testing.T) {
	cleanup := openTestServiceDB(t)
	defer cleanup()

	provider := embedding.NewMockProvider(dao.EmbeddingDim)

	_ = dao.AddCollection("test", "/test", "*.md", nil)
	doc := &dao.DocumentRecord{
		Collection: "test", Path: "a.md", Title: "alpha",
		Body: "alpha body", Hash: "h1", FileSize: 4,
	}
	dao.UpsertDocument(doc)

	chunks := []dao.ChunkData{
		{Content: "alpha alpha", Position: 0, TokenCount: 2, Hash: "h1"},
		{Content: "beta beta", Position: 1, TokenCount: 2, Hash: "h2"},
		{Content: "gamma gamma", Position: 2, TokenCount: 2, Hash: "h3"},
	}
	tokenized := []string{"alpha alpha", "beta beta", "gamma gamma"}
	records, err := dao.InsertChunks(doc.Id, chunks, tokenized)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range records {
		vec, _ := provider.Embed(context.Background(), r.Content)
		if err := dao.InsertVector(r.ID, vec); err != nil {
			t.Fatal(err)
		}
	}

	results := []formatter.SearchHit{
		{ChunkId: records[0].ID, Score: 0.9},
		{ChunkId: records[1].ID, Score: 0.8},
		{ChunkId: records[2].ID, Score: 0.7},
	}

	searcher := NewSearcher(nil)
	got := searcher.ApplyMMR(results, provider, "alpha", 0.5, 2)

	if len(got) != 2 {
		t.Fatalf("expected 2 results, got %d", len(got))
	}

	ids := make(map[int64]bool)
	for _, h := range got {
		if ids[h.ChunkId] {
			t.Fatalf("duplicate chunk ID %d", h.ChunkId)
		}
		ids[h.ChunkId] = true
	}
}

func TestApplyMMRNoEmbeddings(t *testing.T) {
	cleanup := openTestServiceDB(t)
	defer cleanup()

	provider := embedding.NewMockProvider(dao.EmbeddingDim)

	results := []formatter.SearchHit{
		{ChunkId: 999, Score: 0.9},
		{ChunkId: 998, Score: 0.8},
		{ChunkId: 997, Score: 0.7},
	}

	searcher := NewSearcher(nil)
	got := searcher.ApplyMMR(results, provider, "test", 0.7, 2)

	if len(got) != 3 {
		t.Fatalf("expected 3 results (fallback, no embeddings), got %d", len(got))
	}
}
