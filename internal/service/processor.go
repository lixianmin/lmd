package service

import (
	"context"
	"fmt"
	"time"
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
		end := i + batchSize
		if end > len(doc.Chunks) {
			end = len(doc.Chunks)
		}
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

	t := time.Now()
	summary, err := my.generateSummary(ctx, doc.Title, doc.Body)
	if err != nil {
		return fmt.Errorf("generate summary: %w", err)
	}
	summaryDuration := time.Since(t)

	t = time.Now()
	summaryVecs, err := my.embedProvider.EmbedBatch(ctx, []string{summary})
	if err != nil {
		return fmt.Errorf("embed summary: %w", err)
	}
	summaryEmbedDuration := time.Since(t)

	if _, err := dao.UpsertSummaryWithVector(docId, doc.Hash, summary, summary, summaryVecs[0]); err != nil {
		return fmt.Errorf("insert summary: %w", err)
	}

	if err := dao.CompleteDocument(docId, doc.FileModTime); err != nil {
		return fmt.Errorf("complete document: %w", err)
	}

	fullPath := doc.RootDir + "/" + doc.Path
	total := time.Since(totalStart)
	logo.Info("processor: doc %d (%s) chunks=%d embed=%.2fs insert=%.2fs summary_llm=%.2fs summary_embed=%.2fs total=%.2fs",
		docId, fullPath,
		len(doc.Chunks), embedDuration.Seconds(), insertDuration.Seconds(),
		summaryDuration.Seconds(), summaryEmbedDuration.Seconds(), total.Seconds())
	logo.JsonI("summary", summary, "doc", docId, "path", fullPath)

	return nil
}

func (my *Processor) generateSummary(ctx context.Context, title, content string) (string, error) {
	content = my.truncateContent(content)
	prompt := "你是一个知识库索引助手。阅读以下文档，用3-5句话(200-300字)概括其内容、主要论点和结论。\n\n" +
		"文档标题: " + title + "\n" +
		"文档内容:\n" + content + "\n\n" +
		"直接输出摘要，不要加前缀和引号。"

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
