package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/llm"
)

func initSummarizerTestDB(t *testing.T) {
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

func TestSummarizerProcessDoc(t *testing.T) {
	initSummarizerTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	doc := &dao.DocumentRecord{
		Collection: "notes", Path: "test.md", Title: "Test Doc",
		Body: "body text", Hash: "hash1", FileSize: 9,
	}
	if err := dao.UpsertDocument(doc); err != nil {
		t.Fatal(err)
	}

	tokenized := []string{"hello world this is a test chunk"}
	chunks := []dao.ChunkData{
		{Content: "hello world this is a test chunk", Position: 0, TokenCount: 6, Hash: "h1"},
	}
	if _, err := dao.InsertChunks(doc.Id, chunks, tokenized); err != nil {
		t.Fatal(err)
	}

	mockLLM := llm.NewMockLLM("这是一个测试文档的摘要")
	cfg := config.SummaryConfig{
		MaxInputTokens:  30000,
		MaxOutputTokens: 200,
		CooldownSeconds: 60,
	}
	s := NewSummarizer(mockLLM, cfg, nil)

	if err := s.processDoc(context.Background(), doc.Id); err != nil {
		t.Fatalf("processDoc: %v", err)
	}

	if mockLLM.Called != 1 {
		t.Fatalf("expected LLM called once, got %d", mockLLM.Called)
	}

	got, err := dao.GetDocumentBySourceDocId("@summaries", doc.Id)
	if err != nil {
		t.Fatalf("GetDocumentBySourceDocId: %v", err)
	}
	if got.SourceDocId != doc.Id {
		t.Fatalf("expected source_doc_id=%d, got %d", doc.Id, got.SourceDocId)
	}

	chunksAfter, _ := dao.GetChunksByDocId(got.Id)
	if len(chunksAfter) != 1 {
		t.Fatalf("expected 1 summary chunk, got %d", len(chunksAfter))
	}
	if chunksAfter[0].Content != "这是一个测试文档的摘要" {
		t.Fatalf("expected mock summary, got '%s'", chunksAfter[0].Content)
	}
}

func TestSummarizerOnUpsertCallback(t *testing.T) {
	initSummarizerTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	doc := &dao.DocumentRecord{
		Collection: "notes", Path: "test.md", Title: "Test Doc",
		Body: "body text", Hash: "hash1", FileSize: 9,
	}
	dao.UpsertDocument(doc)

	tokenized := []string{"hello world"}
	dao.InsertChunks(doc.Id, []dao.ChunkData{
		{Content: "hello world", Position: 0, TokenCount: 2, Hash: "h1"},
	}, tokenized)

	mockLLM := llm.NewMockLLM("summary text")
	cfg := config.SummaryConfig{
		MaxInputTokens:  30000,
		MaxOutputTokens: 200,
		CooldownSeconds: 60,
	}
	s := NewSummarizer(mockLLM, cfg, nil)

	var callbackCalled int
	s.SetOnUpsert(func() {
		callbackCalled++
	})

	s.processDoc(context.Background(), doc.Id)

	if callbackCalled != 1 {
		t.Fatalf("expected onUpsert called once, got %d", callbackCalled)
	}
}

func TestSummarizerSkipsSystemCollections(t *testing.T) {
	initSummarizerTestDB(t)
	dao.AddCollection("@summaries", "/data", "**/*.md", nil)

	doc := &dao.DocumentRecord{
		Collection: "@summaries", Path: "test.md", Title: "Test",
		Body: "body", Hash: "hash1", FileSize: 4,
	}
	dao.UpsertDocument(doc)

	mockLLM := llm.NewMockLLM("should not be called")
	cfg := config.SummaryConfig{MaxInputTokens: 30000, MaxOutputTokens: 200}
	s := NewSummarizer(mockLLM, cfg, nil)

	err := s.processDoc(context.Background(), doc.Id)
	if err != nil {
		t.Fatalf("expected nil error for system collection skip, got %v", err)
	}
	if mockLLM.Called != 0 {
		t.Fatalf("expected LLM not called for system collection, called %d times", mockLLM.Called)
	}
}

func TestSummarizerSkipsNoChunks(t *testing.T) {
	initSummarizerTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	doc := &dao.DocumentRecord{
		Collection: "notes", Path: "empty.md", Title: "Empty",
		Body: "body", Hash: "hash1", FileSize: 4,
	}
	dao.UpsertDocument(doc)

	mockLLM := llm.NewMockLLM("should not be called")
	cfg := config.SummaryConfig{MaxInputTokens: 30000, MaxOutputTokens: 200}
	s := NewSummarizer(mockLLM, cfg, nil)

	err := s.processDoc(context.Background(), doc.Id)
	if err != nil {
		t.Fatalf("no-chunk doc should return nil (not an error), got %v", err)
	}
	if mockLLM.Called != 0 {
		t.Fatalf("expected LLM not called, called %d times", mockLLM.Called)
	}
}

func TestSummarizerSkipsExistingWithSameHash(t *testing.T) {
	initSummarizerTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	doc := &dao.DocumentRecord{
		Collection: "notes", Path: "test.md", Title: "Test",
		Body: "body", Hash: "hash1", FileSize: 4,
	}
	dao.UpsertDocument(doc)
	dao.InsertChunks(doc.Id, []dao.ChunkData{
		{Content: "content", Position: 0, TokenCount: 1, Hash: "h1"},
	}, []string{"content"})

	dao.UpsertSummaryDoc(doc.Id, "hash1", "old summary", "old summary")

	mockLLM := llm.NewMockLLM("should not be called")
	cfg := config.SummaryConfig{MaxInputTokens: 30000, MaxOutputTokens: 200}
	s := NewSummarizer(mockLLM, cfg, nil)

	err := s.processDoc(context.Background(), doc.Id)
	if err != nil {
		t.Fatalf("expected nil for unchanged doc, got %v", err)
	}
	if mockLLM.Called != 0 {
		t.Fatalf("expected LLM not called for unchanged doc, called %d times", mockLLM.Called)
	}
}

func TestSummarizerDirtyMap(t *testing.T) {
	initSummarizerTestDB(t)
	mockLLM := llm.NewMockLLM("summary")
	cfg := config.SummaryConfig{MaxInputTokens: 30000, MaxOutputTokens: 200}
	s := NewSummarizer(mockLLM, cfg, nil)

	s.addDirty(1)
	s.addDirty(2)

	dirty := s.popDirty()
	if len(dirty) != 2 {
		t.Fatalf("expected 2 dirty, got %d", len(dirty))
	}

	dirty2 := s.popDirty()
	if len(dirty2) != 0 {
		t.Fatalf("expected 0 dirty after pop, got %d", len(dirty2))
	}
}

func TestSummarizerTruncateContent(t *testing.T) {
	mockLLM := llm.NewMockLLM("summary")
	cfg := config.SummaryConfig{MaxInputTokens: 100, MaxOutputTokens: 50}
	s := NewSummarizer(mockLLM, cfg, nil)

	short := "short content"
	if got := s.truncateContent(short); got != short {
		t.Fatalf("short content should not be truncated")
	}
}
