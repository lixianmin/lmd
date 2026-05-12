package service

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/llm"
)

func initProcessorTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := dao.Init(filepath.Join(dir, "test.sqlite")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if dao.DB != nil {
			dao.DB.Close()
			dao.DB = nil
		}
	})
}

func TestProcessNewDoc(t *testing.T) {
	initProcessorTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	mockLLM := llm.NewMockLLM("这是一个测试摘要，包含足够的文字来避免退化检测。")
	mockEmbed := embedding.NewMockProvider(dao.EmbeddingDim)
	cfg := config.HydeConfig{MaxInputTokens: 30000, MaxOutputTokens: 200}
	p := NewProcessor(mockEmbed, mockLLM, cfg)

	doc := PendingDoc{
		Action:      DocNew,
		Collection:  "notes",
		Path:        "test.md",
		Title:       "Test",
		Body:        "hello world this is a test document",
		Hash:        "hash1",
		FileSize:    30,
		FileModTime: 12345,
		Chunks: []dao.ChunkData{
			{Content: "hello world", Position: 0, TokenCount: 2, Hash: "hash1"},
			{Content: "this is a test document", Position: 1, TokenCount: 4, Hash: "hash1"},
		},
	}

	err := p.ProcessDoc(context.Background(), doc)
	if err != nil {
		t.Fatalf("ProcessDoc: %v", err)
	}

	docs, _ := dao.ListDocumentsByCollection("notes")
	if len(docs) != 1 {
		t.Fatalf("expected 1 document, got %d", len(docs))
	}
	if docs[0].FileModTime != 12345 {
		t.Fatalf("expected file_mod_time=12345, got %d", docs[0].FileModTime)
	}

	chunks, _ := dao.GetChunksByDocId(docs[0].Id)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	summaryDoc, err := dao.GetDocumentBySourceDocId("@summaries", docs[0].Id)
	if err != nil {
		t.Fatalf("GetDocumentBySourceDocId: %v", err)
	}
	if summaryDoc == nil {
		t.Fatal("expected summary document to exist")
	}

	if mockLLM.Called != 1 {
		t.Fatalf("expected LLM called once, got %d", mockLLM.Called)
	}
}

func TestProcessDeletedDoc(t *testing.T) {
	initProcessorTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	docId, _ := dao.InsertDocument("notes", "test.md", "Title", "body", 4, "h1")
	dao.CompleteDocument(docId, 12345)

	cfg := config.HydeConfig{MaxInputTokens: 30000, MaxOutputTokens: 200}
	p := NewProcessor(embedding.NewMockProvider(dao.EmbeddingDim), llm.NewMockLLM("summary"), cfg)

	doc := PendingDoc{
		Action:   DocDeleted,
		OldDocId: docId,
	}

	err := p.ProcessDoc(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := dao.GetDocumentById(docId); err == nil {
		t.Fatal("document should be deleted")
	}
}

func TestProcessChangedDoc(t *testing.T) {
	initProcessorTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	oldDocId, _ := dao.InsertDocument("notes", "test.md", "Old Title", "old body", 8, "old_hash")
	dao.CompleteDocument(oldDocId, 1000)

	cfg := config.HydeConfig{MaxInputTokens: 30000, MaxOutputTokens: 200}
	mockLLM := llm.NewMockLLM("这是一段新的摘要内容，足够长以通过退化检测。")
	p := NewProcessor(embedding.NewMockProvider(dao.EmbeddingDim), mockLLM, cfg)

	doc := PendingDoc{
		Action:      DocChanged,
		Collection:  "notes",
		Path:        "test.md",
		Title:       "New Title",
		Body:        "new body content",
		Hash:        "new_hash",
		FileSize:    17,
		FileModTime: 2000,
		OldDocId:    oldDocId,
		Chunks:      []dao.ChunkData{{Content: "new body content", Position: 0, TokenCount: 3, Hash: "new_hash"}},
	}

	err := p.ProcessDoc(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := dao.GetDocumentById(oldDocId); err == nil {
		t.Fatal("old document should be deleted")
	}

	docs, _ := dao.ListDocumentsByCollection("notes")
	if len(docs) != 1 {
		t.Fatalf("expected 1 new document, got %d", len(docs))
	}
	if docs[0].Hash != "new_hash" {
		t.Fatalf("expected hash 'new_hash', got '%s'", docs[0].Hash)
	}
	if docs[0].FileModTime != 2000 {
		t.Fatalf("expected file_mod_time=2000, got %d", docs[0].FileModTime)
	}
}

func TestIsDegenerate(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		expect bool
	}{
		{"normal", "这是一段正常的摘要内容，包含多个不同的句子和词汇。", false},
		{"short", "太短", true},
		{"empty", "", true},
		{"repeated", strings.Repeat("nanaa nanaa ", 40), true},
		{"normal_long", "文档介绍了Elasticsearch的基本使用方法，包括索引创建、查询语法和聚合分析。主要内容包括：如何使用REST API进行文档的增删改查，如何构建复杂的布尔查询，以及如何使用桶聚合和指标聚合进行数据分析。结论是Elasticsearch是一个功能强大的搜索引擎，适合处理大规模数据的全文检索和分析需求。", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isDegenerate(tt.text)
			if got != tt.expect {
				t.Errorf("isDegenerate(%q) = %v, want %v", tt.text, got, tt.expect)
			}
		})
	}
}
