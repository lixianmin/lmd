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

func (my *Searcher) SearchLex(query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	ftsQuery := query
	if my.tokenizer != nil {
		ftsQuery = my.tokenizer.TokenizeToString(query)
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
			ChunkId:    r.ChunkID,
			DocId:      dao.ShortDocId(r.DocId),
			Collection: r.Collection,
			Path:       r.Path,
			Title:      r.Title,
			Score:      r.Score,
			Snippet:    r.Content,
			Line:       r.Line,
		})
	}

	return hits, nil
}

func (my *Searcher) SearchVector(provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	logo.Info("SearchVector: query=%q collection=%s limit=%d", query, collection, limit)
	queryVec, err := provider.EmbedQuery(context.Background(), query)
	if err != nil {
		return nil, err
	}
	return my.SearchVectorByEmbedding(queryVec, collection, limit), nil
}

func (my *Searcher) SearchVectorWithPRF(provider embedding.EmbeddingProvider, query, collection string, limit int, ftsHits []formatter.SearchHit) ([]formatter.SearchHit, error) {
	logo.Info("SearchVectorWithPRF: query=%q collection=%s ftsHits=%d", query, collection, len(ftsHits))
	queryVec, err := provider.EmbedQuery(context.Background(), query)
	if err != nil {
		return nil, err
	}

	if len(ftsHits) >= 3 {
		var chunkIds []int64
		for i := 0; i < 3 && i < len(ftsHits); i++ {
			chunkIds = append(chunkIds, ftsHits[i].ChunkId)
		}

		embeddings, err := dao.GetEmbeddingsByChunkIds(chunkIds)
		if err == nil && len(embeddings) > 0 {
			var docVecs [][]float32
			for _, e := range embeddings {
				docVecs = append(docVecs, e.Embedding)
			}
			queryVec = Rocchio(queryVec, docVecs, 0.6, 0.4)
			logo.Info("SearchVectorWithPRF: Rocchio applied with %d feedback docs", len(docVecs))
		}
	}

	return my.SearchVectorByEmbedding(queryVec, collection, limit), nil
}

func (my *Searcher) SearchVectorByEmbedding(queryVec []float32, collection string, limit int) []formatter.SearchHit {
	vecResults, err := dao.QueryVectors(queryVec, limit)
	if err != nil {
		return nil
	}

	var hits []formatter.SearchHit
	for _, r := range vecResults {
		score := dao.SimilarityToScore(r.Distance)

		chunk, err := dao.GetChunkById(r.ChunkID)
		if err != nil {
			continue
		}

		doc, err := dao.GetDocumentById(chunk.DocId)
		if err != nil {
			continue
		}

		if collection != "" && doc.Collection != collection {
			continue
		}

		hits = append(hits, formatter.SearchHit{
			ChunkId:    r.ChunkID,
			DocId:      dao.ShortDocId(doc.DocId),
			Collection: doc.Collection,
			Path:       doc.Path,
			Title:      doc.Title,
			Score:      score,
			Snippet:    chunk.Content,
			Line:       chunk.Position,
		})
	}

	return hits
}
