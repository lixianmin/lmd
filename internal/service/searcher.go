package service

import (
	"context"
	"fmt"

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
	hits, err := my.SearchVectorByEmbedding(queryVec, collection, limit)
	if err != nil {
		return nil, err
	}
	if minScore > 0 {
		var filtered []formatter.SearchHit
		for _, h := range hits {
			if h.Score >= minScore {
				filtered = append(filtered, h)
			}
		}
		return filtered, nil
	}
	return hits, nil
}

func (my *Searcher) SearchVectorByEmbedding(queryVec []float32, collection string, limit int) ([]formatter.SearchHit, error) {
	vecResults, err := dao.QueryVectors(queryVec, limit)
	if err != nil {
		return nil, fmt.Errorf("vector query failed: %w", err)
	}

	if len(vecResults) == 0 {
		return nil, nil
	}

	chunkIds := make([]int64, len(vecResults))
	for i, r := range vecResults {
		chunkIds[i] = r.ChunkID
	}

	chunks, err := dao.GetChunksByIds(chunkIds)
	if err != nil {
		return nil, fmt.Errorf("fetch chunks failed: %w", err)
	}

	docIds := make(map[int64]struct{})
	for _, c := range chunks {
		docIds[c.DocId] = struct{}{}
	}
	docIdSlice := make([]int64, 0, len(docIds))
	for id := range docIds {
		docIdSlice = append(docIdSlice, id)
	}
	docs, err := dao.GetDocumentsByIds(docIdSlice)
	if err != nil {
		return nil, fmt.Errorf("fetch docs failed: %w", err)
	}

	chunkMap := make(map[int64]*dao.ChunkRecord)
	for i := range chunks {
		chunkMap[chunks[i].ID] = &chunks[i]
	}
	docMap := make(map[int64]*dao.DocumentRecord)
	for i := range docs {
		docMap[docs[i].Id] = &docs[i]
	}

	distanceMap := make(map[int64]float64)
	for _, r := range vecResults {
		distanceMap[r.ChunkID] = r.Distance
	}

	var hits []formatter.SearchHit
	for _, r := range vecResults {
		chunk, ok := chunkMap[r.ChunkID]
		if !ok {
			continue
		}
		doc, ok := docMap[chunk.DocId]
		if !ok {
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
			Score:      dao.SimilarityToScore(distanceMap[r.ChunkID]),
			Snippet:    chunk.Content,
			Line:       chunk.Position,
		})
	}

	return hits, nil
}
