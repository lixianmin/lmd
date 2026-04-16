package service

import (
	"context"
	"fmt"
	"io"
	"os"
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
	provider embedding.EmbeddingProvider
}

func NewEmbedder(provider embedding.EmbeddingProvider) *Embedder {
	return &Embedder{provider: provider}
}

func (my *Embedder) EmbedBatch(ctx context.Context, limit int) (*EmbedResult, error) {
	result := &EmbedResult{}

	chunks, err := dao.GetUnembeddedChunks(limit)
	if err != nil {
		return nil, err
	}

	if len(chunks) == 0 {
		fmt.Fprintf(os.Stderr, "  No chunks to embed (all up to date).\n")
		return result, nil
	}

	logo.Info("EmbedAll: embedding %d chunks", len(chunks))

	totalChunks, embeddedCount := dao.GetChunkCounts()
	alreadyDone := embeddedCount

	fmt.Fprintf(os.Stderr, "  Embedding %d chunks (est. %d min)...\n", len(chunks), len(chunks)*2/8/60+1)
	printProgress(os.Stderr, alreadyDone, totalChunks)

	const maxChunksPerBatch = 8

	start := 0
	for start < len(chunks) {
		end := start + maxChunksPerBatch
		if end > len(chunks) {
			end = len(chunks)
		}

		batch := chunks[start:end]
		texts := make([]string, len(batch))
		for i, c := range batch {
			t := c.Content
			runes := []rune(t)
			if len(runes) > 800 {
				t = string(runes[:800])
			}
			texts[i] = t
		}

		t0 := time.Now()
		vecs, err := my.provider.EmbedBatch(ctx, texts)
		embedDur := time.Since(t0)
		batchDur := time.Since(t0)
		if err != nil {
			logo.Error("EmbedAll: batch [%d:%d] (%d chunks) failed embed in %s: %s", start, end, len(batch), embedDur, err)
			result.Failed += len(batch)
			start = end
			continue
		}

		items := make([]struct {
			ChunkId   int64
			Embedding []float32
		}, len(vecs))
		for i, vec := range vecs {
			items[i].ChunkId = batch[i].ID
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

		done := alreadyDone + result.Embedded + result.Failed
		printProgress(os.Stderr, done, totalChunks)
		logo.Info("EmbedAll: batch [%d:%d] %d chunks embed=%s insert=%s total=%s", start, end, len(batch), embedDur, insertDur, batchDur)

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
