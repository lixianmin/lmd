package service

import (
	"context"
	"fmt"
	"io"
	"os"

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
	provider embedding.EmbeddingProvider
}

func NewEmbedder(provider embedding.EmbeddingProvider) *Embedder {
	return &Embedder{provider: provider}
}

func (e *Embedder) EmbedBatch(ctx context.Context, limit int) (*EmbedResult, error) {
	result := &EmbedResult{}

	chunks, err := dao.GetUnembeddedChunks(limit)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		return result, nil
	}

	logo.Info("EmbedAll: embedding %d chunks", len(chunks))

	totalChunks, embeddedCount := dao.GetChunkCounts()
	alreadyDone := embeddedCount

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
			if err := dao.InsertVector(batch[i].ID, vec); err != nil {
				result.Failed++
				continue
			}
			result.Embedded++
		}

		done := alreadyDone + result.Embedded + result.Failed
		printProgress(os.Stderr, done, totalChunks)

		start = end
	}

	fmt.Fprintf(os.Stderr, "\n")
	logo.Info("EmbedAll: done embedded=%d failed=%d", result.Embedded, result.Failed)
	return result, nil
}

func printProgress(w io.Writer, done, total int) {
	const width = 30
	pct := float64(done) / float64(total)
	filled := int(pct * float64(width))
	bar := ""
	for i := 0; i < width; i++ {
		if i < filled {
			bar += "█"
		} else {
			bar += "░"
		}
	}
	fmt.Fprintf(w, "\r  [%s] %d/%d (%.1f%%)", bar, done, total, pct*100)
}
