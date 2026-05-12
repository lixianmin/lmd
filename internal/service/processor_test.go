package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
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

	mockEmbed := embedding.NewMockProvider(dao.EmbeddingDim)
	p := NewProcessor(mockEmbed)

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
}

func TestProcessDeletedDoc(t *testing.T) {
	initProcessorTestDB(t)
	dao.AddCollection("notes", "/data", "**/*.md", nil)

	docId, _ := dao.InsertDocument("notes", "test.md", "Title", "body", 4, "h1")
	dao.CompleteDocument(docId, 12345)

	p := NewProcessor(embedding.NewMockProvider(dao.EmbeddingDim))

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

	p := NewProcessor(embedding.NewMockProvider(dao.EmbeddingDim))

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

