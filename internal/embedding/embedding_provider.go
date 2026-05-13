package embedding

import (
	"context"
)

const EmbeddingDim = 1024 // 向量维度，与 dao.EmbeddingDim 保持一致

type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
	Dimension() int
	ModelName() string
}
