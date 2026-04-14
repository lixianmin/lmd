package service

import (
	"database/sql"
	"strings"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

type Searcher struct {
	db        *sql.DB
	tokenizer tokenizer.Tokenizer
}

func NewSearcher(db *sql.DB, tok tokenizer.Tokenizer) *Searcher {
	return &Searcher{db: db, tokenizer: tok}
}

func (s *Searcher) SearchLex(query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	var tokenized string
	if s.tokenizer != nil {
		tokens := s.tokenizer.Cut(query)
		filtered := s.filterStopTokens(tokens)
		if len(filtered) > 0 {
			tokenized = strings.Join(filtered, " OR ")
		}
	} else {
		tokenized = query
	}

	if tokenized == "" {
		return nil, nil
	}

	ftsResults, err := store.SearchFTS(s.db, tokenized, collection, limit)
	if err != nil {
		return nil, err
	}

	var hits []formatter.SearchHit
	for _, r := range ftsResults {
		if r.Score < minScore {
			continue
		}

		doc, err := store.GetDocumentByDocID(s.db, r.DocID)
		if err != nil {
			continue
		}

		snippet := extractSnippet(doc.Body, query, 200)
		line := findLineNumber(doc.Body, query)

		hits = append(hits, formatter.SearchHit{
			DocID:      r.DocID,
			Collection: r.Collection,
			Path:       r.Path,
			Title:      r.Title,
			Score:      r.Score,
			Snippet:    snippet,
			Line:       line,
		})
	}

	return hits, nil
}

func (s *Searcher) SearchVector(provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	queryVec, err := provider.EmbedQuery(nil, query)
	if err != nil {
		return nil, err
	}

	vecResults, err := store.QueryVectors(s.db, queryVec, limit)
	if err != nil {
		return nil, err
	}

	var hits []formatter.SearchHit
	for _, r := range vecResults {
		score := store.SimilarityToScore(r.Distance)
		if score < minScore {
			continue
		}

		chunk, err := store.GetChunkByID(s.db, r.ChunkID)
		if err != nil {
			continue
		}

		doc, err := store.GetDocumentByID(s.db, chunk.DocID)
		if err != nil {
			continue
		}

		if collection != "" && doc.Collection != collection {
			continue
		}

		hits = append(hits, formatter.SearchHit{
			DocID:      doc.DocID,
			Collection: doc.Collection,
			Path:       doc.Path,
			Title:      doc.Title,
			Score:      score,
			Snippet:    chunk.Content,
			Line:       1,
		})
	}

	if len(hits) > 0 {
		topScore := hits[0].Score
		for i := range hits {
			hits[i].Score = store.NormalizeScore(hits[i].Score, topScore)
		}
	}

	return hits, nil
}

func (s *Searcher) SearchHybrid(provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	lexHits, err := s.SearchLex(query, collection, limit*3, 0)
	if err != nil {
		return nil, err
	}

	vecHits, err := s.SearchVector(provider, query, collection, limit*3, 0)
	if err != nil {
		return nil, err
	}

	fused := FuseRRF(lexHits, vecHits, 60, 1.0)

	var results []formatter.SearchHit
	for _, h := range fused {
		if h.Score < minScore {
			continue
		}
		results = append(results, h)
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

func extractSnippet(body, query string, maxLen int) string {
	idx := strings.Index(strings.ToLower(body), strings.ToLower(query))
	if idx == -1 {
		if len(body) > maxLen {
			return body[:maxLen] + "..."
		}
		return body
	}

	start := idx - maxLen/3
	if start < 0 {
		start = 0
	}
	end := start + maxLen
	if end > len(body) {
		end = len(body)
	}

	snippet := body[start:end]
	if start > 0 {
		snippet = "..." + snippet
	}
	if end < len(body) {
		snippet = snippet + "..."
	}
	return snippet
}

func findLineNumber(body, query string) int {
	idx := strings.Index(strings.ToLower(body), strings.ToLower(query))
	if idx == -1 {
		return 1
	}
	return strings.Count(body[:idx], "\n") + 1
}

var stopTokens = map[string]bool{
	"的": true, "了": true, "在": true, "是": true, "我": true,
	"有": true, "和": true, "就": true, "不": true, "人": true,
	"都": true, "一": true, "一个": true, "上": true, "也": true,
	"很": true, "到": true, "说": true, "要": true, "去": true,
	"你": true, "会": true, "着": true, "没有": true, "看": true,
	"好": true, "自己": true, "这": true,
	"哪些": true, "什么": true, "怎么": true, "如何": true,
	"哪": true, "几": true, "多少": true,
	"吗": true, "呢": true, "吧": true, "啊": true, "呀": true,
	"可以": true, "能": true,
}

func (s *Searcher) filterStopTokens(tokens []string) []string {
	var filtered []string
	for _, t := range tokens {
		if !stopTokens[t] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
