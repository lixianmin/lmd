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

	keywords := my.extractKeywords(doc.Body)

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
	logo.Info("hyde: %s\n  QUESTIONS:\n%s\n  KEYWORDS: %s", doc.Path, strings.Join(questions, "\n  "), strings.Join(keywords, ", "))
	return nil
}

func (my *HyDEIndexer) generateQuestions(ctx context.Context, content string) ([]string, error) {
	maxRunes := my.maxInput * 3
	runes := []rune(content)
	if len(runes) > maxRunes {
		runes = runes[:maxRunes]
	}
	truncated := string(runes)

	prompt := "根据下面的文档，生成 5-10 个该文档可以回答的问题。" +
		"关注文档中提到的具体事实、细节和信息。" +
		"每行一个问题。要具体——询问名称、数字、地点、日期、颜色、偏好等。" +
		"重要：用与文档相同的语言编写问题。" +
		"不要重复相同的词或短语，正常书写。\n\n" +
		"文档：\n" + truncated + "\n\n问题："

	messages := []llm.Message{{Role: "user", Content: prompt}}
	maxRetries := 3

	for attempt := range maxRetries {
		response, err := my.llm.ChatCompletion(ctx, messages)
		if err != nil {
			if attempt == maxRetries-1 {
				return nil, err
			}
			continue
		}

		if isRepetitive(response) {
			if attempt < maxRetries-1 {
				continue
			}
			return nil, fmt.Errorf("repetitive output after %d retries", maxRetries)
		}

		lines := strings.Split(strings.TrimSpace(response), "\n")
		var questions []string
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if len(line) > 10 && (strings.Contains(line, "?") || strings.Contains(line, "？")) {
				questions = append(questions, line)
			}
		}

		if len(questions) > 0 {
			return questions, nil
		}

		if attempt < maxRetries-1 {
			continue
		}
		return nil, fmt.Errorf("no valid questions after %d retries", maxRetries)
	}

	return nil, fmt.Errorf("failed to generate questions")
}

func isRepetitive(s string) bool {
	words := strings.Fields(s)
	if len(words) < 10 {
		return false
	}
	seen := make(map[string]int, len(words))
	for _, w := range words {
		seen[strings.ToLower(w)]++
	}
	return float64(len(seen))/float64(len(words)) < 0.2
}

func (my *HyDEIndexer) extractKeywords(content string) []string {
	type freq struct {
		word  string
		count int
	}

	words := my.tokenizer.Cut(content)

	freqs := make(map[string]int)
	for _, w := range words {
		w = strings.Trim(w, ".,?!;:()[]{}'\"-")
		if len([]rune(w)) < 2 {
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
