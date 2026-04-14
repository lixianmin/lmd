package service

import (
	"context"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

type Searcher struct {
	tokenizer tokenizer.Tokenizer
}

func NewSearcher(tok tokenizer.Tokenizer) *Searcher {
	return &Searcher{tokenizer: tok}
}

func (s *Searcher) SearchLex(query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	ftsQuery := query
	if s.tokenizer != nil {
		ftsQuery = s.tokenizer.TokenizeToString(query)
		if ftsQuery == "" {
			ftsQuery = query
		}
	}

	ftsResults, err := dao.SearchFTS(ftsQuery, collection, limit)
	if err != nil {
		return nil, err
	}

	var hits []formatter.SearchHit
	for _, r := range ftsResults {
		if r.Score < minScore {
			continue
		}

		hits = append(hits, formatter.SearchHit{
			DocId:      dao.ShortDocId(r.DocId),
			Collection: r.Collection,
			Path:       r.Path,
			Title:      r.Title,
			Score:      r.Score,
			Snippet:    r.Content,
			Line:       1,
		})
	}

	return hits, nil
}

func (s *Searcher) SearchVector(provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	logo.Info("SearchVector: query=%q collection=%s limit=%d", query, collection, limit)
	queryVec, err := provider.EmbedQuery(context.Background(), query)
	if err != nil {
		return nil, err
	}

	vecResults, err := dao.QueryVectors(queryVec, limit)
	if err != nil {
		return nil, err
	}

	var hits []formatter.SearchHit
	for _, r := range vecResults {
		score := dao.SimilarityToScore(r.Distance)
		if score < minScore {
			continue
		}

		chunk, err := dao.GetChunkById(r.ChunkID)
		if err != nil {
			continue
		}

		doc, err := dao.GetDocumentByID(chunk.DocId)
		if err != nil {
			continue
		}

		if collection != "" && doc.Collection != collection {
			continue
		}

		hits = append(hits, formatter.SearchHit{
			DocId:      dao.ShortDocId(doc.DocId),
			Collection: doc.Collection,
			Path:       doc.Path,
			Title:      doc.Title,
			Score:      score,
			Snippet:    chunk.Content,
			Line:       1,
		})
	}

	return hits, nil
}

func (s *Searcher) SearchHybrid(provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	logo.Info("SearchHybrid: query=%q collection=%s limit=%d", query, collection, limit)
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
