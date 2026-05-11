package service

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/llm"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

const summaryCollection = "@summaries"

type Summarizer struct {
	llm           llm.LLMProvider
	maxOutput     int
	maxInput      int
	tokenizer     tokenizer.Tokenizer
	embedProvider embedding.EmbeddingProvider
}

func NewSummarizer(llmProvider llm.LLMProvider, cfg config.SummaryConfig, tok tokenizer.Tokenizer, embedProv embedding.EmbeddingProvider) *Summarizer {
	var my = &Summarizer{
		llm:           llmProvider,
		maxOutput:     cfg.MaxOutputTokens,
		maxInput:      cfg.MaxInputTokens,
		tokenizer:     tok,
		embedProvider: embedProv,
	}

	return my
}

func (my *Summarizer) ScanPendingDocs() []int64 {
	cols, err := dao.ListCollections()
	if err != nil {
		logo.Warn("summarizer: list collections error: %s", err)
		return nil
	}

	type docEntry struct {
		id int64
	}

	var candidates []docEntry
	for _, col := range cols {
		if strings.HasPrefix(col.Name, "@") {
			continue
		}
		docs, err := dao.ListDocumentsByCollection(col.Name)
		if err != nil {
			continue
		}
		for _, doc := range docs {
			if doc.Id == 0 {
				continue
			}
			candidates = append(candidates, docEntry{id: doc.Id})
		}
	}

	var pending []int64
	for _, c := range candidates {
		existing, _ := dao.GetDocumentBySourceDocId(summaryCollection, c.id)
		if existing != nil {
			continue
		}

		pending = append(pending, c.id)
	}

	if len(pending) > 0 {
		logo.Info("summarizer: found %d docs without summary", len(pending))
	}
	return pending
}

func (my *Summarizer) ProcessDoc(ctx context.Context, docId int64) error {
	doc, err := dao.GetDocumentById(docId)
	if err != nil {
		logo.Warn("summarizer: get doc %d error: %s", docId, err)
		return err
	}

	if len(doc.Collection) > 0 && doc.Collection[0] == '@' {
		return nil
	}

	existingSummary, _ := dao.GetDocumentBySourceDocId(summaryCollection, docId)
	if existingSummary != nil {
		existingChunks, _ := dao.GetChunksByDocId(existingSummary.Id)
		if len(existingChunks) > 0 && existingChunks[0].Hash == doc.Hash {
			dao.TouchDocument(existingSummary.Id)
			return nil
		}
	}

	chunks, err := dao.GetChunksByDocId(docId)
	if err != nil || len(chunks) == 0 {
		logo.Warn("summarizer: no chunks for doc %d", docId)
		return err
	}

	var content string
	for _, c := range chunks {
		content += c.Content + "\n"
	}

	content = my.truncateContent(content)

	summary, err := my.generateSummary(ctx, doc.Title, content)
	if err != nil {
		logo.Warn("summarizer: generate summary for doc %d error: %s", docId, err)
		return err
	}

	vecs, err := my.embedProvider.EmbedBatch(ctx, []string{summary})
	if err != nil {
		logo.Warn("summarizer: embed summary for doc %d error: %s", docId, err)
		return err
	}

	logo.Info("summarizer: generated summary for doc %d (%s) → %s", docId, doc.Title, summary)
	return my.upsertSummaryWithVector(docId, doc.Hash, summary, vecs[0])
}

func (my *Summarizer) truncateContent(content string) string {
	promptOverhead := 200
	available := my.maxInput - promptOverhead - my.maxOutput
	if available <= 0 {
		available = 1000
	}

	contentBytes := len(content)
	contentTokens := contentBytes / 2

	if contentTokens <= available {
		return content
	}

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

func (my *Summarizer) generateSummary(ctx context.Context, title, content string) (string, error) {
	prompt := "你是一个知识库索引助手。阅读以下文档，用1-2句话(不超过100字)概括其内容和核心主题。\n\n" +
		"文档标题: " + title + "\n" +
		"文档内容:\n" + content + "\n\n" +
		"请直接输出摘要，不要加前缀和引号。"

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	return my.llm.ChatCompletion(ctx, messages)
}

func (my *Summarizer) upsertSummaryWithVector(sourceDocId int64, hash, summary string, vec []float32) error {
	tokenized := summary
	if my.tokenizer != nil {
		if t := my.tokenizer.TokenizeToString(summary); t != "" {
			tokenized = t
		}
	}
	docId, err := dao.UpsertSummaryWithVector(sourceDocId, hash, summary, tokenized, vec)
	if err != nil {
		logo.Error("summarizer: upsert summary with vector for doc %d failed: %s", sourceDocId, err)
		return err
	}
	logo.Info("summarizer: upserted summary+vector for sourceDoc %d → summaryDoc %d", sourceDocId, docId)
	return nil
}
