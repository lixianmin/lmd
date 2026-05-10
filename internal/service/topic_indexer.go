package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/logo"
)

type docPreview struct {
	Path    string
	Title   string
	Preview string
}

type parsedTopic struct {
	Title          string
	Overview       string
	KeyTopics      []string
	Documents      []topicDoc
	SemanticGroups []topicGroup
}

type topicDoc struct {
	Path string
	Desc string
}

type topicGroup struct {
	Name  string
	Count int
}

type TopicIndexer struct {
	llm      *LLMClient
	provider embedding.EmbeddingProvider
	cooldown time.Duration
}

func NewTopicIndexer(llm *LLMClient, provider embedding.EmbeddingProvider, cooldown time.Duration) *TopicIndexer {
	return &TopicIndexer{
		llm:      llm,
		provider: provider,
		cooldown: cooldown,
	}
}

func (my *TopicIndexer) SummarizeDir(ctx context.Context, collection, dirPath, relPath string) error {
	docs, err := my.gatherDocs(collection, relPath)
	if err != nil {
		return err
	}
	if len(docs) == 0 {
		logo.Info("TopicIndexer: skip empty dir %s/%s", collection, relPath)
		return nil
	}

	logo.Info("TopicIndexer: summarizing %s/%s (%d docs)", collection, relPath, len(docs))

	prompt := buildSummarizePrompt(relPath, docs)
	markdown, genErr := my.llm.Generate(prompt, 2048)
	if genErr != nil {
		return fmt.Errorf("llm generate failed: %w", genErr)
	}

	topicPath := filepath.Join(dirPath, "_topic.md")
	if err := os.WriteFile(topicPath, []byte(markdown), 0644); err != nil {
		return fmt.Errorf("write _topic.md failed: %w", err)
	}

	return my.storeTopic(ctx, collection, relPath, markdown)
}

func (my *TopicIndexer) gatherDocs(collection, prefix string) ([]docPreview, error) {
	docs, err := dao.ListDocumentsByCollection(collection)
	if err != nil {
		return nil, err
	}

	const maxDocsPerTopic = 50
	var previews []docPreview
	for _, d := range docs {
		if d.Path == "_topic.md" || filepath.Base(d.Path) == "_topic.md" {
			continue
		}

		docDir := filepath.Dir(d.Path)
		expectedDir := prefix
		if expectedDir == "" {
			expectedDir = "."
		}
		if docDir != expectedDir && docDir != "." && prefix == "" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(d.Path, prefix) {
			continue
		}

		body := d.Body
		runes := []rune(body)
		preview := body
		// previewRunLen scales down for large dirs to keep prompt within context
		previewRunLen := 200
		if len(runes) > previewRunLen {
			preview = string(runes[:previewRunLen])
		}

		previews = append(previews, docPreview{
			Path:    filepath.Base(d.Path),
			Title:   d.Title,
			Preview: preview,
		})
		if len(previews) >= maxDocsPerTopic {
			logo.Warn("TopicIndexer: dir %s/%s has %d docs, truncating to %d", collection, prefix, len(docs), maxDocsPerTopic)
			break
		}
	}
	return previews, nil
}

func buildSummarizePrompt(dirPath string, docs []docPreview) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("你是一个知识库索引助手。请阅读以下目录中的文档标题和摘要，生成一个 _topic.md 索引文件。\n\n目录: %s\n文档数量: %d\n\n文档列表:\n", dirPath, len(docs)))
	for _, d := range docs {
		b.WriteString(fmt.Sprintf("--- %s: %s\n%s\n", d.Path, d.Title, d.Preview))
	}
	b.WriteString("\n请按以下格式生成 _topic.md（只输出 markdown，不要额外解释）：\n\n# <简短目录标题>\n> <2-3句概述>\n## 关键主题\n- <5-8个核心主题词>\n## 文档\n- `filename.md` — <一句话描述>\n## 语义分组\n- **<分组名>** (N篇): file1.md, file2.md, ...\n")
	return b.String()
}

func parseTopicMD(markdown string) (*parsedTopic, error) {
	t := &parsedTopic{}
	lines := strings.Split(markdown, "\n")
	inKeyTopics := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "# ") && !strings.HasPrefix(line, "## "):
			t.Title = strings.TrimPrefix(line, "# ")
		case strings.HasPrefix(line, "> "):
			if t.Overview == "" {
				t.Overview = strings.TrimPrefix(line, "> ")
			}
		case line == "## 关键主题":
			inKeyTopics = true
		case strings.HasPrefix(line, "## "):
			inKeyTopics = false
		case inKeyTopics && strings.HasPrefix(line, "- "):
			t.KeyTopics = append(t.KeyTopics, strings.TrimPrefix(line, "- "))
		case isDocLine(line):
			doc := parseDocLine(line)
			t.Documents = append(t.Documents, doc)
		case strings.HasPrefix(line, "- **") && (strings.Contains(line, "篇") || strings.Contains(line, "):")):
			group := parseGroupLine(line)
			if group.Name != "" {
				t.SemanticGroups = append(t.SemanticGroups, group)
			}
		}
	}
	return t, nil
}

func isDocLine(line string) bool {
	if !strings.HasPrefix(line, "- ") {
		return false
	}
	if strings.Contains(line, "篇") {
		return false
	}
	for _, sep := range []string{" — ", " - "} {
		if strings.Index(line, sep) > 1 {
			return true
		}
	}
	return false
}

func parseDocLine(line string) topicDoc {
	path := ""
	desc := ""
	for _, sep := range []string{" — ", " - "} {
		idx := strings.Index(line, sep)
		if idx >= 0 {
			path = strings.TrimSpace(line[2:idx])
			path = strings.Trim(path, "`")
			desc = strings.TrimSpace(line[idx+len(sep):])
			break
		}
	}
	return topicDoc{Path: path, Desc: desc}
}

var groupRe = regexp.MustCompile(`\*\*(.+?)\*\*`)

func parseGroupLine(line string) topicGroup {
	matches := groupRe.FindStringSubmatch(line)
	name := ""
	if len(matches) > 1 {
		name = matches[1]
	}
	return topicGroup{Name: name}
}

func (my *TopicIndexer) storeTopic(ctx context.Context, collection, relPath, markdown string) error {
	parsed, err := parseTopicMD(markdown)
	if err != nil {
		return fmt.Errorf("parse _topic.md failed: %w", err)
	}

	hash := Sha256Hex(markdown)

	var docPaths []string
	for _, d := range parsed.Documents {
		docPaths = append(docPaths, d.Path)
	}
	docPathsJSON := "[" + strings.Join(quoteStrings(docPaths), ",") + "]"

	if err := dao.UpsertTopic(collection, relPath, parsed.Overview, docPathsJSON, hash); err != nil {
		return fmt.Errorf("upsert topic failed: %w", err)
	}

	rowID, err := dao.GetTopicRowID(collection, relPath)
	if err != nil {
		return fmt.Errorf("get topic rowid failed: %w", err)
	}

	_ = dao.DeleteTopicVectorByRowID(rowID)

	if my.provider != nil {
		vec, err := my.provider.Embed(ctx, parsed.Overview)
		if err != nil {
			logo.Warn("TopicIndexer: embed topic overview failed: %s", err)
			return nil
		}
		if err := dao.UpsertTopicVector(rowID, vec); err != nil {
			logo.Warn("TopicIndexer: upsert topic vector failed: %s", err)
			return nil
		}
	}
	return nil
}

func Sha256Hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func ShouldSummarize(existingHash, newHash string) bool {
	if existingHash == "" {
		return true
	}
	return existingHash == newHash
}

func quoteStrings(ss []string) []string {
	result := make([]string, len(ss))
	for i, s := range ss {
		result[i] = `"` + s + `"`
	}
	return result
}
