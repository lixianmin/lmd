package service

import (
	"context"
	"database/sql"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/logo"
)

type EmbedResult struct {
	Embedded int
	Skipped  int
	Failed   int
}

type Embedder struct {
	db       *sql.DB
	provider embedding.EmbeddingProvider
}

func NewEmbedder(db *sql.DB, provider embedding.EmbeddingProvider) *Embedder {
	return &Embedder{db: db, provider: provider}
}

func (e *Embedder) EmbedAll(ctx context.Context) (*EmbedResult, error) {
	result := &EmbedResult{}

	chunks, err := store.GetUnembeddedChunks(e.db)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		return result, nil
	}

	logo.Info("EmbedAll: embedding %d chunks", len(chunks))

	const maxTokensPerBatch = 3500

	start := 0
	for start < len(chunks) {
		batchTokens := 0
		end := start
		for end < len(chunks) {
			tokens := chunks[end].TokenCount
			if tokens <= 0 {
				tokens = len(chunks[end].Content) / 3
			}
			if end > start && batchTokens+tokens > maxTokensPerBatch {
				break
			}
			batchTokens += tokens
			end++
		}

		batch := chunks[start:end]
		texts := make([]string, len(batch))
		for i, c := range batch {
			texts[i] = c.Content
		}

		vecs, err := e.provider.EmbedBatch(ctx, texts)
		if err != nil {
			logo.Error("EmbedAll: batch [%d:%d] (%d tokens) failed: %s", start, end, batchTokens, err)
			result.Failed += len(batch)
			start = end
			continue
		}

		for i, vec := range vecs {
			if err := store.InsertVector(e.db, batch[i].ID, vec); err != nil {
				result.Failed++
				continue
			}
			result.Embedded++
		}
		start = end
	}

	logo.Info("EmbedAll: done embedded=%d failed=%d", result.Embedded, result.Failed)
	return result, nil
}
