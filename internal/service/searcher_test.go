package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/dao"
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

	results, err := searcher.SearchLex("搜索引擎", "", 10, 0, "")
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

	results, err := searcher.SearchLex("搜索引擎", "nonexistent", 10, 0, "")
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

	results, err := searcher.SearchLex("Hello", "", 10, 0, "")
	if err != nil {
		t.Fatalf("SearchLex failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for 'Hello'")
	}
}
