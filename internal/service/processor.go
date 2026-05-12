package service

import (
	"context"
	"fmt"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/logo"
)

type Processor struct {
	embedProvider embedding.EmbeddingProvider
}

func NewProcessor(embedProv embedding.EmbeddingProvider) *Processor {
	return &Processor{
		embedProvider: embedProv,
	}
}

func (my *Processor) ProcessDoc(ctx context.Context, doc PendingDoc) error {
	if doc.Action != DocDeleted && !dao.CollectionExists(doc.Collection) {
		logo.Info("processor: skip doc, collection %q removed", doc.Collection)
		return nil
	}

	switch doc.Action {
	case DocDeleted:
		logo.Info("processor: deleting doc %d (%s/%s)", doc.OldDocId, doc.Collection, doc.Path)
		return dao.DeleteDocumentAndSummary(doc.OldDocId)
	case DocChanged:
		return my.processDocChanged(ctx, doc)
	case DocNew:
		return my.processDocNew(ctx, doc)
	}
	return nil
}

func (my *Processor) processDocChanged(ctx context.Context, doc PendingDoc) error {
	if err := dao.DeleteDocumentAndSummary(doc.OldDocId); err != nil {
		return fmt.Errorf("delete old doc: %w", err)
	}
	return my.processDocNew(ctx, doc)
}

func (my *Processor) processDocNew(ctx context.Context, doc PendingDoc) error {
	totalStart := time.Now()

	docId, err := dao.InsertDocument(doc.Collection, doc.Path, doc.Title, doc.Body, doc.FileSize, doc.Hash)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	var embedDuration time.Duration
	var insertDuration time.Duration
	batchSize := 8
	for i := 0; i < len(doc.Chunks); i += batchSize {
		end := min(i+batchSize, len(doc.Chunks))
		batch := doc.Chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
		}

		t := time.Now()
		vecs, err := my.embedProvider.EmbedBatch(ctx, texts)
		embedDuration += time.Since(t)
		if err != nil {
			return fmt.Errorf("embed chunks batch %d: %w", i/batchSize, err)
		}

		tokenized := make([]string, len(batch))
		for j, c := range batch {
			tokenized[j] = c.Content
		}

		t = time.Now()
		if _, err := dao.InsertChunksAndVectors(docId, doc.Collection, i, batch, tokenized, vecs); err != nil {
			return fmt.Errorf("insert chunks batch %d: %w", i/batchSize, err)
		}
		insertDuration += time.Since(t)
	}

	if err := dao.CompleteDocument(docId, doc.FileModTime); err != nil {
		return fmt.Errorf("complete document: %w", err)
	}

	fullPath := doc.RootDir + "/" + doc.Path
	total := time.Since(totalStart)
	logo.Info("processor: doc %d (%s) chunks=%d embed=%.2fs insert=%.2fs total=%.2fs",
		docId, fullPath,
		len(doc.Chunks), embedDuration.Seconds(), insertDuration.Seconds(),
		total.Seconds())

	return nil
}


