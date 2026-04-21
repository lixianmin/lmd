package embedding

import (
	"context"
)

const EmbedQueryPrefix = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: "

type EmbeddingProvider interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
	Dimension() int
	ModelName() string
	Close() error
}
