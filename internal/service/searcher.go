package service

import (
	"database/sql"
	"strings"

	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

type SearchHit struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
}

type Searcher struct {
	db        *sql.DB
	tokenizer tokenizer.Tokenizer
}

func NewSearcher(db *sql.DB, tok tokenizer.Tokenizer) *Searcher {
	return &Searcher{db: db, tokenizer: tok}
}

func (s *Searcher) SearchLex(query, collection string, limit int, minScore float64) ([]SearchHit, error) {
	var tokenized string
	if s.tokenizer != nil {
		tokenized = s.tokenizer.TokenizeToString(query)
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

	var hits []SearchHit
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

		hits = append(hits, SearchHit{
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
