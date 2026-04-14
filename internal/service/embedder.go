package service

import (
	"database/sql"
	"fmt"

	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/store"
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

func (e *Embedder) EmbedAll(modelName string, force bool) (*EmbedResult, error) {
	result := &EmbedResult{}

	if force {
		return nil, fmt.Errorf("force re-embed not yet implemented")
	}

	chunks, err := store.GetUnembeddedChunks(e.db, modelName)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		total, _ := store.CountEmbedded(e.db, modelName)
		result.Skipped = total
		return result, nil
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	vecs, err := e.provider.EmbedBatch(nil, texts)
	if err != nil {
		return nil, err
	}

	for i, vec := range vecs {
		if err := store.InsertVector(e.db, chunks[i].ID, vec); err != nil {
			result.Failed++
			continue
		}
		if err := store.MarkEmbedded(e.db, chunks[i].ID, modelName); err != nil {
			result.Failed++
			continue
		}
		result.Embedded++
	}

	return result, nil
}
