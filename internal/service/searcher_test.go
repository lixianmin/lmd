package service

import (
	"testing"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

func TestSearchBM25(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewChunkIndexer(tok)
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
	idx := NewChunkIndexer(tok)
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
	idx := NewChunkIndexer(tok)
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

func TestSearchPosMust(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewChunkIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	searcher := NewSearcher(tok)

	results, err := searcher.SearchLex("搜索引擎", "", 10, 0, "pos-must")
	if err != nil {
		t.Fatalf("SearchLex pos-must failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for '搜索引擎' with pos-must")
	}

	found := false
	for _, r := range results {
		if r.Collection == "test" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected results from test collection with pos-must")
	}
}

func TestSearchPosOr(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewChunkIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	searcher := NewSearcher(tok)

	results, err := searcher.SearchLex("搜索引擎", "", 10, 0, "pos-or")
	if err != nil {
		t.Fatalf("SearchLex pos-or failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for '搜索引擎' with pos-or")
	}
}

func TestSearchPosWeight(t *testing.T) {
	_, dir, cleanup := setupIndexTest(t)
	defer cleanup()

	_ = dao.AddCollection("test", dir, "*.md", nil)
	tok, _ := tokenizer.NewGseTokenizer()
	idx := NewChunkIndexer(tok)
	_, _ = idx.UpdateCollection("test", dir, "*.md", nil)

	searcher := NewSearcher(tok)

	results, err := searcher.SearchLex("搜索引擎", "", 10, 0, "pos-weight")
	if err != nil {
		t.Fatalf("SearchLex pos-weight failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for '搜索引擎' with pos-weight")
	}
}

func TestBuildFTSQueryPosMust(t *testing.T) {
	tok, _ := tokenizer.NewGseTokenizer()
	searcher := NewSearcher(tok)

	query := "便宜的苹果手机降价了"

	got := searcher.buildFTSQueryPosMust(query)

	if got == "" {
		t.Fatal("expected non-empty query")
	}

	// 名词（苹果、手机）应该用 AND 连接
	if !containsWord(got, "苹果") {
		t.Errorf("expected 苹果 in query, got: %s", got)
	}
	if !containsWord(got, "手机") {
		t.Errorf("expected 手机 in query, got: %s", got)
	}

	// pos-must 查询应该包含 AND 操作符（名词 MUST）
	if !containsWord(got, "AND") {
		t.Errorf("pos-must query should contain AND, got: %s", got)
	}
}

func TestBuildFTSQueryPosOr(t *testing.T) {
	tok, _ := tokenizer.NewGseTokenizer()
	searcher := NewSearcher(tok)

	query := "便宜的苹果手机降价了"

	got := searcher.buildFTSQueryPosOr(query)

	if got == "" {
		t.Fatal("expected non-empty query")
	}

	if !containsWord(got, "OR") {
		t.Errorf("pos-or query should use OR, got: %s", got)
	}

	// 应包含苹果、手机（名词）
	if !containsWord(got, "苹果") {
		t.Errorf("expected 苹果 in query, got: %s", got)
	}
	if !containsWord(got, "手机") {
		t.Errorf("expected 手机 in query, got: %s", got)
	}
}

func TestApplyPosWeight(t *testing.T) {
	tok, _ := tokenizer.NewGseTokenizer()
	searcher := NewSearcher(tok)

	hits := []formatter.SearchHit{
		{ChunkId: 1, Score: 0.5, Snippet: "苹果手机真的很不错"},
		{ChunkId: 2, Score: 0.5, Snippet: "这个降价幅度很大"},
		{ChunkId: 3, Score: 0.5, Snippet: "完全无关的内容"},
	}

	result := searcher.applyPosWeight("苹果手机降价了", hits)

	if len(result) != 3 {
		t.Fatalf("expected 3 results, got %d", len(result))
	}

	// 包含两个名词（苹果、手机）的 hit 1 应该排第一
	if result[0].ChunkId != 1 {
		t.Errorf("expected chunk 1 (名词最多) 排第一，got chunk %d: score=%.3f", result[0].ChunkId, result[0].Score)
	}

	// hit 1 score 应该 > 原始 score
	if result[0].Score <= 0.5 {
		t.Errorf("expected boosted score > 0.5, got %.3f", result[0].Score)
	}
}

func TestGSEPosForEnglish(t *testing.T) {
	tok, _ := tokenizer.NewGseTokenizer()

	english := "What type of bulb did I replace in my bedside lamp"
	pos := tok.Pos(english)

	if len(pos) == 0 {
		t.Fatal("expected POS tags for English text")
	}

	// GSE 对英文词通常标记为 "x" (unknown)
	allX := true
	for _, p := range pos {
		t.Logf("%s/%s", p.Text, p.Pos)
		if p.Pos != "x" {
			allX = false
		}
	}

	if allX {
		t.Log("All English tokens tagged as 'x' (unknown) — pos-must == pos-or for English")
	} else {
		t.Log("Some English tokens have non-x POS tags")
	}

	chinese := "便宜的苹果手机降价了"
	pos2 := tok.Pos(chinese)
	for _, p := range pos2 {
		t.Logf("%s/%s", p.Text, p.Pos)
	}
}

func containsTermMUST(query, word string) bool {
	target := "+" + word
	for _, term := range fieldsAnySpace(query) {
		if term == target {
			return true
		}
	}
	return false
}

func containsWord(query, word string) bool {
	for _, term := range fieldsAnySpace(query) {
		if term == word {
			return true
		}
	}
	return false
}

func fieldsAnySpace(s string) []string {
	var result []string
	word := ""
	for _, r := range s {
		if r == ' ' || r == '\t' {
			if word != "" {
				result = append(result, word)
				word = ""
			}
		} else {
			word += string(r)
		}
	}
	if word != "" {
		result = append(result, word)
	}
	return result
}
