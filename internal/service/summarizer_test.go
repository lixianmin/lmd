package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lixianmin/lmd/internal/config"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
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
	}
	s := NewSummarizer(mockLLM, cfg, nil, embedding.NewMockProvider(dao.EmbeddingDim))

	if err := s.ProcessDoc(context.Background(), doc.Id); err != nil {
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

	var vecCount int
	rows, _ := dao.WithQuery("SELECT COUNT(*) FROM chunks_vec WHERE chunk_id=?", chunksAfter[0].Id)
	if rows.Next() {
		rows.Scan(&vecCount)
	}
	rows.Close()
	if vecCount != 1 {
		t.Fatalf("expected 1 vector, got %d", vecCount)
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
	s := NewSummarizer(mockLLM, cfg, nil, nil)

	err := s.ProcessDoc(context.Background(), doc.Id)
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
	s := NewSummarizer(mockLLM, cfg, nil, nil)

	err := s.ProcessDoc(context.Background(), doc.Id)
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
	s := NewSummarizer(mockLLM, cfg, nil, nil)

	err := s.ProcessDoc(context.Background(), doc.Id)
	if err != nil {
		t.Fatalf("expected nil for unchanged doc, got %v", err)
	}
	if mockLLM.Called != 0 {
		t.Fatalf("expected LLM not called for unchanged doc, called %d times", mockLLM.Called)
	}
}

func TestSummarizerRecoverDirtyDocs(t *testing.T) {
	initSummarizerTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	doc1 := &dao.DocumentRecord{Collection: "notes", Path: "a.md", Title: "A", Body: "body", Hash: "h1", FileSize: 4}
	doc2 := &dao.DocumentRecord{Collection: "notes", Path: "b.md", Title: "B", Body: "body", Hash: "h2", FileSize: 4}
	dao.UpsertDocument(doc1)
	dao.UpsertDocument(doc2)
	dao.InsertChunks(doc1.Id, []dao.ChunkData{{Content: "c1", Position: 0, TokenCount: 1, Hash: "h1"}}, []string{"c1"})
	dao.InsertChunks(doc2.Id, []dao.ChunkData{{Content: "c2", Position: 0, TokenCount: 1, Hash: "h2"}}, []string{"c2"})

	mockLLM := llm.NewMockLLM("summary")
	cfg := config.SummaryConfig{MaxInputTokens: 30000, MaxOutputTokens: 200}
	s := NewSummarizer(mockLLM, cfg, nil, nil)

	pending := s.ScanPendingDocs()
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	pending2 := s.ScanPendingDocs()
	if len(pending2) != 2 {
		t.Fatalf("expected 2 pending on second call, got %d", len(pending2))
	}
}

func TestSummarizerTruncateContent(t *testing.T) {
	mockLLM := llm.NewMockLLM("summary")
	cfg := config.SummaryConfig{MaxInputTokens: 100, MaxOutputTokens: 50}
	s := NewSummarizer(mockLLM, cfg, nil, nil)

	short := "short content"
	if got := s.truncateContent(short); got != short {
		t.Fatalf("short content should not be truncated")
	}
}
