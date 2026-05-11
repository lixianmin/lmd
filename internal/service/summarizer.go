package service

import (
	"context"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/llm"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

const summaryCollection = "@summaries"

type Summarizer struct {
	mu        sync.Mutex
	dirty     map[int64]bool
	llm       llm.LLMProvider
	maxOutput int
	maxInput  int
	onUpsert  func()
	stopCh    <-chan struct{}
	tokenizer tokenizer.Tokenizer
}

func NewSummarizer(llmProvider llm.LLMProvider, cfg config.SummaryConfig, tok tokenizer.Tokenizer) *Summarizer {
	return &Summarizer{
		dirty:     make(map[int64]bool),
		llm:       llmProvider,
		maxOutput: cfg.MaxOutputTokens,
		maxInput:  cfg.MaxInputTokens,
		tokenizer: tok,
	}
}

func (my *Summarizer) SetOnUpsert(fn func()) {
	my.onUpsert = fn
}

func (my *Summarizer) SetStopCh(ch <-chan struct{}) {
	my.stopCh = ch
}

func (my *Summarizer) MarkDirty(docIds []int64) {
	my.mu.Lock()
	defer my.mu.Unlock()
	for _, id := range docIds {
		my.dirty[id] = true
	}
}

func (my *Summarizer) ScanAll() {
	cols, err := dao.ListCollections()
	if err != nil {
		logo.Warn("summarizer: list collections error: %s", err)
		return
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

	var missing int
	for _, c := range candidates {
		if my.isDirty(c.id) {
			continue
		}

		existing, _ := dao.GetDocumentBySourceDocId(summaryCollection, c.id)
		if existing != nil {
			continue
		}

		my.addDirty(c.id)
		missing++
	}

	if missing > 0 {
		logo.Info("summarizer: scan found %d docs without summary, marked dirty", missing)
	}
}

func (my *Summarizer) ProcessDirty() {
	dirty := my.popDirty()
	if len(dirty) == 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	if my.stopCh != nil {
		go func() {
			select {
			case <-my.stopCh:
				cancel()
			case <-ctx.Done():
			}
		}()
	}
	defer cancel()

	logo.Info("summarizer: processing %d dirty docs", len(dirty))
	var done, failed int
	for docId := range dirty {
		if err := my.processDoc(ctx, docId); err != nil {
			failed++
		} else {
			done++
		}
	}
	logo.Info("summarizer: done processing %d docs (%d ok, %d failed)", len(dirty), done, failed)
}

func (my *Summarizer) addDirty(id int64) {
	my.mu.Lock()
	my.dirty[id] = true
	my.mu.Unlock()
}

func (my *Summarizer) isDirty(id int64) bool {
	my.mu.Lock()
	ok := my.dirty[id]
	my.mu.Unlock()
	return ok
}

func (my *Summarizer) popDirty() map[int64]bool {
	my.mu.Lock()
	defer my.mu.Unlock()

	if len(my.dirty) == 0 {
		return nil
	}

	dirty := my.dirty
	my.dirty = make(map[int64]bool)

	return dirty
}

func (my *Summarizer) processDoc(ctx context.Context, docId int64) error {
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

	logo.Info("summarizer: generated summary for doc %d (%s) → %s", docId, doc.Title, summary)
	my.upsertSummary(docId, doc.Hash, summary)
	if my.onUpsert != nil {
		my.onUpsert()
	}
	return nil
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

func (my *Summarizer) upsertSummary(sourceDocId int64, hash, summary string) {
	tokenized := summary
	if my.tokenizer != nil {
		if t := my.tokenizer.TokenizeToString(summary); t != "" {
			tokenized = t
		}
	}
	_, err := dao.UpsertSummaryDoc(sourceDocId, hash, summary, tokenized)
	if err != nil {
		logo.Error("summarizer: upsert summary for doc %d failed: %s", sourceDocId, err)
	}
}
