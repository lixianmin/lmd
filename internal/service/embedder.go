package service

import (
	"context"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/logo"
)

type EmbedResult struct {
	Embedded int
	Skipped  int
	Failed   int
}

type Embedder struct {
	provider   embedding.EmbeddingProvider
	batchSize  int
	truncation int // rune 截断上限，超过此长度的 chunk 文本会被截断后再送入 embedding 模型
}

func NewEmbedder(provider embedding.EmbeddingProvider, batchSize, truncation int) *Embedder {
	return &Embedder{provider: provider, batchSize: batchSize, truncation: truncation}
}

func (my *Embedder) EmbedBatch(ctx context.Context, limit int) (*EmbedResult, error) {
	result := &EmbedResult{}

	chunks, err := dao.GetUnembeddedChunks(limit)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		logo.Info("EmbedAll: no chunks to embed (all up to date)")
		return result, nil
	}

	logo.Info("EmbedAll: embedding %d chunks", len(chunks))

	const fallbackBatchSize = 8
	batchSize := my.batchSize
	if batchSize <= 0 {
		batchSize = fallbackBatchSize
	}

	start := 0
	for start < len(chunks) {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}

		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		batch := chunks[start:end]
		texts := make([]string, len(batch))
		for i, c := range batch {
			t := c.Content
			if my.truncation > 0 {
				runes := []rune(t)
				if len(runes) > my.truncation {
					t = string(runes[:my.truncation])
				}
			}
			texts[i] = t
		}

		t0 := time.Now()
		vecs, err := my.provider.EmbedBatch(ctx, texts)
		embedDur := time.Since(t0)
		if err != nil {
			logo.Error("EmbedAll: batch [%d:%d] (%d chunks) failed embed in %s: %s", start, end, len(batch), embedDur, err)
			result.Failed += len(batch)
			start = end
			continue
		}

		items := make([]struct {
			ChunkId    int64
			DocId      int64
			Collection string
			Embedding  []float32
		}, len(vecs))

		docIds := make(map[int64]bool)
		for _, c := range batch {
			docIds[c.DocId] = true
		}
		ids := make([]int64, 0, len(docIds))
		for id := range docIds {
			ids = append(ids, id)
		}
		docs, _ := dao.GetDocumentsByIds(ids)
		collectionMap := make(map[int64]string)
		for _, d := range docs {
			collectionMap[d.Id] = d.Collection
		}

		for i, vec := range vecs {
			items[i].ChunkId = batch[i].Id
			items[i].DocId = batch[i].DocId
			items[i].Collection = collectionMap[batch[i].DocId]
			items[i].Embedding = vec
		}

		t1 := time.Now()
		if err := dao.InsertVectors(items); err != nil {
			logo.Error("EmbedAll: batch [%d:%d] insert failed: %s", start, end, err)
			result.Failed += len(batch)
			start = end
			continue
		}
		insertDur := time.Since(t1)
		result.Embedded += len(vecs)

		logo.Info("EmbedAll: batch [%d:%d] %d chunks embed=%s insert=%s total=%s", start, end, len(batch), embedDur, insertDur, time.Since(t0))

		start = end
	}

	logo.Info("EmbedAll: done embedded=%d failed=%d", result.Embedded, result.Failed)
	return result, nil
}
