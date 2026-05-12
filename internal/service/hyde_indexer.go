package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/llm"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

type HyDEIndexer struct {
	llm       llm.LLMProvider
	embedProv embedding.EmbeddingProvider
	tokenizer tokenizer.Tokenizer
	maxOutput int
	maxInput  int
}

func NewHyDEIndexer(llmProv llm.LLMProvider, embedProv embedding.EmbeddingProvider, tok tokenizer.Tokenizer, cfg config.HydeConfig) *HyDEIndexer {
	return &HyDEIndexer{
		llm:       llmProv,
		embedProv: embedProv,
		tokenizer: tok,
		maxOutput: cfg.MaxOutputTokens,
		maxInput:  cfg.MaxInputTokens,
	}
}

func (my *HyDEIndexer) ProcessDoc(ctx context.Context, doc PendingDoc) error {
	if doc.Action == DocDeleted {
		return nil
	}

	t0 := time.Now()

	questions, err := my.generateQuestions(ctx, doc.Body)
	if err != nil {
		return fmt.Errorf("generate questions: %w", err)
	}

	keywords := extractKeywords(doc.Body)

	content := "QUESTIONS:\n" + strings.Join(questions, "\n") + "\n\nKEYWORDS:\n" + strings.Join(keywords, ", ")

	vecs, err := my.embedProv.EmbedBatch(ctx, []string{content})
	if err != nil {
		return fmt.Errorf("embed hyde data: %w", err)
	}

	sourceDoc, err := dao.GetDocumentByPath(doc.Collection, doc.Path)
	if err != nil || sourceDoc == nil {
		return fmt.Errorf("find source doc %s/%s: %w", doc.Collection, doc.Path, err)
	}

	docHash := doc.Hash
	if _, err := dao.UpsertHydeData(sourceDoc.Id, docHash, content, content, vecs[0]); err != nil {
		return fmt.Errorf("insert hyde data: %w", err)
	}

	logo.JsonI("hyde_doc", sourceDoc.Id, "path", doc.Path, "questions", len(questions), "keywords", len(keywords), "elapsed", time.Since(t0).String())
	return nil
}

func (my *HyDEIndexer) generateQuestions(ctx context.Context, content string) ([]string, error) {
	truncated := content
	maxChars := 8000
	if len(truncated) > maxChars {
		truncated = truncated[:maxChars]
	}

	prompt := "Given the document below, generate 5-10 questions that this document could answer. " +
		"Focus on specific facts, details, and information mentioned in the text. " +
		"One question per line. Be specific — ask about names, numbers, places, dates, colors, preferences.\n\n" +
		"Document:\n" + truncated + "\n\nQuestions:"

	messages := []llm.Message{{Role: "user", Content: prompt}}

	response, err := my.llm.ChatCompletion(ctx, messages)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(response), "\n")
	var questions []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 10 && strings.Contains(line, "?") {
			questions = append(questions, line)
		}
	}

	if len(questions) == 0 {
		questions = append(questions, response)
	}

	return questions, nil
}

func extractKeywords(content string) []string {
	type freq struct {
		word  string
		count int
	}

	freqs := make(map[string]int)
	words := strings.Fields(content)
	for _, w := range words {
		w = strings.Trim(w, ".,?!;:()[]{}'\"-")
		if len(w) <= 2 {
			continue
		}
		if _, ok := stopWords[strings.ToLower(w)]; ok {
			continue
		}
		freqs[w]++
	}

	list := make([]freq, 0, len(freqs))
	for w, c := range freqs {
		list = append(list, freq{w, c})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].count > list[j].count })

	top := min(30, len(list))
	result := make([]string, 0, top)
	for i := 0; i < top; i++ {
		result = append(result, list[i].word)
	}

	return result
}

func (my *HyDEIndexer) ScanChanges(collectionName, rootDir, globPattern string, ignorePatterns []string) ([]PendingDoc, error) {
	ci := &ChunkIndexer{tokenizer: my.tokenizer}
	return ci.ScanChanges(collectionName, rootDir, globPattern, ignorePatterns)
}
