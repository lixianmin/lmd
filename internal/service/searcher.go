package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

// ftsSafeRe 移除 FTS5 不认识的字符，只保留字母、数字、空格
var ftsSafeRe = regexp.MustCompile(`[^a-zA-Z0-9\s\p{Han}\p{Katakana}\p{Hiragana}]`) 

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
	// FTS5 不允许 ? [] {} 等特殊字符, 保留字母数字 * " 和空格
	ftsQuery = strings.TrimSpace(ftsSafeRe.ReplaceAllString(ftsQuery, ""))
	if ftsQuery == "" {
		return nil, nil
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

func (my *Searcher) SearchVector(ctx context.Context, provider embedding.EmbeddingProvider, query, collection string, limit int, minScore float64) ([]formatter.SearchHit, error) {
	logo.Info("SearchVector: query=%q collection=%s limit=%d", query, collection, limit)
	queryVec, err := provider.EmbedQuery(ctx, query)
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

const vectorOverfetchFactor = 5 // 向量搜索全局取回后按 collection 过滤，需放大取回量以保证足够结果

func (my *Searcher) SearchVectorByEmbedding(queryVec []float32, collection string, limit int) ([]formatter.SearchHit, error) {
	fetchLimit := limit
	if collection != "" {
		fetchLimit = limit * vectorOverfetchFactor
	}

	vecResults, err := dao.QueryVectors(queryVec, fetchLimit)
	if err != nil {
		return nil, fmt.Errorf("vector query failed: %w", err)
	}

	if len(vecResults) == 0 {
		return nil, nil
	}

	chunkIds := make([]int64, len(vecResults))
	for i, r := range vecResults {
		chunkIds[i] = r.ChunkId
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
		chunkMap[chunks[i].Id] = &chunks[i]
	}
	docMap := make(map[int64]*dao.DocumentRecord)
	for i := range docs {
		docMap[docs[i].Id] = &docs[i]
	}

	distanceMap := make(map[int64]float64)
	for _, r := range vecResults {
		distanceMap[r.ChunkId] = r.Distance
	}

	var hits []formatter.SearchHit
	for _, r := range vecResults {
		chunk, ok := chunkMap[r.ChunkId]
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
			ChunkId:    r.ChunkId,
			DocId:      dao.ShortDocId(doc.DocId),
			Collection: doc.Collection,
			Path:       doc.Path,
			Title:      doc.Title,
			Score:      dao.SimilarityToScore(distanceMap[r.ChunkId]),
			Snippet:    chunk.Content,
			Line:       chunk.Position,
		})
	}

	return hits, nil
}
