package service

import (
	"context"
	"fmt"
	"unicode/utf8"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/llm"
	"github.com/lixianmin/logo"
)

type Processor struct {
	embedProvider embedding.EmbeddingProvider
	llm           llm.LLMProvider
	maxOutput     int
	maxInput      int
}

func NewProcessor(embedProv embedding.EmbeddingProvider, llmProv llm.LLMProvider, cfg config.SummaryConfig) *Processor {
	return &Processor{
		embedProvider: embedProv,
		llm:           llmProv,
		maxOutput:     cfg.MaxOutputTokens,
		maxInput:      cfg.MaxInputTokens,
	}
}

func (my *Processor) ProcessDoc(ctx context.Context, doc PendingDoc) error {
	switch doc.Action {
	case DocDeleted:
		logo.Info("processor: deleting doc %d (%s/%s)", doc.OldDocId, doc.Collection, doc.Path)
		return dao.DeleteDocumentAndSummary(doc.OldDocId)
	case DocNew, DocChanged:
		return my.processNewOrChanged(ctx, doc)
	}
	return nil
}

func (my *Processor) processNewOrChanged(ctx context.Context, doc PendingDoc) error {
	if doc.Action == DocChanged {
		if err := dao.DeleteDocumentAndSummary(doc.OldDocId); err != nil {
			return fmt.Errorf("delete old doc: %w", err)
		}
	}

	docId, err := dao.InsertDocument(doc.Collection, doc.Path, doc.Title, doc.Body, doc.FileSize, doc.Hash)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	batchSize := 20 // embedding batch size
	for i := 0; i < len(doc.Chunks); i += batchSize {
		end := i + batchSize
		if end > len(doc.Chunks) {
			end = len(doc.Chunks)
		}
		batch := doc.Chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
		}

		vecs, err := my.embedProvider.EmbedBatch(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed chunks batch %d: %w", i/batchSize, err)
		}

		tokenized := make([]string, len(batch))
		for j, c := range batch {
			tokenized[j] = c.Content
		}

		if _, err := dao.InsertChunksAndVectors(docId, doc.Collection, batch, tokenized, vecs); err != nil {
			return fmt.Errorf("insert chunks batch %d: %w", i/batchSize, err)
		}
	}

	summary, err := my.generateSummary(ctx, doc.Title, doc.Body)
	if err != nil {
		return fmt.Errorf("generate summary: %w", err)
	}

	summaryVecs, err := my.embedProvider.EmbedBatch(ctx, []string{summary})
	if err != nil {
		return fmt.Errorf("embed summary: %w", err)
	}

	if _, err := dao.UpsertSummaryWithVector(docId, doc.Hash, summary, summary, summaryVecs[0]); err != nil {
		return fmt.Errorf("insert summary: %w", err)
	}

	if err := dao.CompleteDocument(docId, doc.FileModTime); err != nil {
		return fmt.Errorf("complete document: %w", err)
	}

	logo.Info("processor: completed doc %d (%s/%s)", docId, doc.Collection, doc.Path)
	return nil
}

func (my *Processor) generateSummary(ctx context.Context, title, content string) (string, error) {
	content = my.truncateContent(content)
	prompt := "你是一个知识库索引助手。阅读以下文档，用1-2句话(不超过100字)概括其内容和核心主题。\n\n" +
		"文档标题: " + title + "\n" +
		"文档内容:\n" + content + "\n\n" +
		"请直接输出摘要，不要加前缀和引号。"

	messages := []llm.Message{{Role: "user", Content: prompt}}
	return my.llm.ChatCompletion(ctx, messages)
}

func (my *Processor) truncateContent(content string) string {
	promptOverhead := 200 // prompt模板的预估token开销
	available := my.maxInput - promptOverhead - my.maxOutput
	if available <= 0 {
		available = 1000
	}

	// 粗估：1 token ≈ 2 bytes（中英混合）
	contentBytes := len(content)
	contentTokens := contentBytes / 2

	if contentTokens <= available {
		return content
	}

	// 截断策略：head 60% + tail 40%
	headRatio := 0.6
	headBytes := int(float64(available) * headRatio * 2)
	tailBytes := int(float64(available) * (1 - headRatio) * 2)

	head := content
	if headBytes < len(head) {
		head = head[:headBytes]
		for len(head) > 0 && !utf8.ValidString(head) {
			head = head[:len(head)-1]
		}
	}

	tail := content
	if len(tail) > tailBytes {
		tail = tail[len(tail)-tailBytes:]
		for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
			tail = tail[1:]
		}
	}

	return head + "\n...(truncated)...\n" + tail
}
