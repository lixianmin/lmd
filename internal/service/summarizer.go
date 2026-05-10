package service

import (
	"context"
	"sync"
	"unicode/utf8"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/llm"
	"github.com/lixianmin/logo"
)

const summaryCollection = "@summaries"

type Summarizer struct {
	mu        sync.Mutex
	dirty     map[int64]bool
	llm       llm.LLMProvider
	maxOutput int
	maxInput  int
}

func NewSummarizer(llmProvider llm.LLMProvider, cfg config.SummaryConfig) *Summarizer {
	return &Summarizer{
		dirty:     make(map[int64]bool),
		llm:       llmProvider,
		maxOutput: cfg.MaxOutputTokens,
		maxInput:  cfg.MaxInputTokens,
	}
}

func (my *Summarizer) MarkDirty(docIds []int64) {
	my.mu.Lock()
	defer my.mu.Unlock()
	for _, id := range docIds {
		my.dirty[id] = true
	}
}

func (my *Summarizer) ProcessDirty() {
	my.mu.Lock()
	dirty := my.dirty
	my.dirty = make(map[int64]bool)
	my.mu.Unlock()

	if len(dirty) == 0 {
		return
	}

	for docID := range dirty {
		my.processDoc(docID)
	}
}

func (my *Summarizer) processDoc(docID int64) {
	doc, err := dao.GetDocumentById(docID)
	if err != nil {
		logo.Warn("summarizer: get doc %d error: %s", docID, err)
		return
	}

	if len(doc.Collection) > 0 && doc.Collection[0] == '@' {
		return
	}

	existingSummary, _ := my.findExistingSummary(docID)
	if existingSummary != nil {
		existingChunks, _ := dao.GetChunksByDocId(existingSummary.Id)
		if len(existingChunks) > 0 && existingChunks[0].Hash == doc.Hash {
			dao.TouchDocument(existingSummary.Id)
			return
		}
	}

	chunks, err := dao.GetChunksByDocId(docID)
	if err != nil || len(chunks) == 0 {
		logo.Warn("summarizer: no chunks for doc %d", docID)
		return
	}

	var content string
	for _, c := range chunks {
		content += c.Content + "\n"
	}

	content = my.truncateContent(content)

	summary, err := my.generateSummary(doc.Title, content)
	if err != nil {
		logo.Warn("summarizer: generate summary for doc %d error: %s", docID, err)
		return
	}

	my.upsertSummary(docID, doc.Hash, summary)
}

func (my *Summarizer) findExistingSummary(docID int64) (*dao.DocumentRecord, error) {
	return dao.GetDocumentBySourceDocId(summaryCollection, docID)
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

func (my *Summarizer) generateSummary(title, content string) (string, error) {
	prompt := "你是一个知识库索引助手。阅读以下文档，用1-2句话(不超过100字)概括其内容和核心主题。\n\n" +
		"文档标题: " + title + "\n" +
		"文档内容:\n" + content + "\n\n" +
		"请直接输出摘要，不要加前缀和引号。"

	messages := []llm.Message{
		{Role: "user", Content: prompt},
	}

	ctx := context.Background()
	return my.llm.ChatCompletion(ctx, messages)
}

func (my *Summarizer) upsertSummary(sourceDocID int64, hash, summary string) {
	existing, _ := my.findExistingSummary(sourceDocID)
	if existing != nil {
		dao.DeleteDocument(existing.Id)
	}

	doc := &dao.DocumentRecord{
		Collection:  summaryCollection,
		Path:        "",
		Title:       "",
		Body:        "",
		Hash:        hash,
		SourceDocId: sourceDocID,
	}

	if err := dao.UpsertDocument(doc); err != nil {
		logo.Warn("summarizer: upsert summary doc error: %s", err)
		return
	}

	chunks := []dao.ChunkData{{
		Content:    summary,
		Position:   0,
		TokenCount: 0,
		Hash:       hash,
	}}

	_, err := dao.InsertChunks(doc.Id, chunks, []string{summary})
	if err != nil {
		logo.Warn("summarizer: insert summary chunk error: %s", err)
	}
}
