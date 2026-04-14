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

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vecs, err := e.provider.EmbedBatch(ctx, texts)
	if err != nil {
		logo.Error("EmbedAll: batch embedding failed: %s", err)
		return nil, err
	}

	logo.Info("EmbedAll: received %d vectors from API", len(vecs))

	for i, vec := range vecs {
		if err := store.InsertVector(e.db, chunks[i].ID, vec); err != nil {
			result.Failed++
			continue
		}
		result.Embedded++
	}

	return result, nil
}
