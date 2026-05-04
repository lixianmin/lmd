package dao

import (
	"testing"
)

func TestInsertMemoryContent(t *testing.T) {
	initTestDB(t)

	id, err := InsertMemory("user prefers dark mode")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	rec, err := GetMemoryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Content != "user prefers dark mode" {
		t.Fatalf("expected 'user prefers dark mode', got %q", rec.Content)
	}
	if rec.Collection != "@episodic" {
		t.Fatalf("expected '@episodic', got %q", rec.Collection)
	}
}

func TestGetMemoryByIDNotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetMemoryByID(99999)
	if err == nil {
		t.Fatal("expected error for non-existent memory")
	}
}

func TestDeleteMemory(t *testing.T) {
	initTestDB(t)

	doc := &DocumentRecord{
		DocId:      "test-delete-doc",
		Collection: "@episodic",
		Path:       "/@memory/test-delete",
		Hash:       "test-delete-hash",
		Body:       "delete me",
	}
	if err := UpsertDocument(doc); err != nil {
		t.Fatal(err)
	}

	chunks, err := InsertChunks(doc.Id, []ChunkData{{
		Content: "delete me", Position: 1, TokenCount: 2, Hash: "test-delete-hash",
	}}, []string{"delete me"})
	if err != nil {
		t.Fatal(err)
	}

	if err := DeleteMemory(doc.Id); err != nil {
		t.Fatalf("DeleteMemory failed: %v", err)
	}

	_, err = GetMemoryByID(doc.Id)
	if err == nil {
		t.Fatal("memory should not exist after delete")
	}

	_, err = GetChunkById(chunks[0].Id)
	if err == nil {
		t.Fatal("chunks should be deleted")
	}
}

func TestDeleteMemoryNotFound(t *testing.T) {
	initTestDB(t)

	err := DeleteMemory(99999)
	if err == nil {
		t.Fatal("expected error for non-existent memory")
	}
}

func TestUpdateMemory(t *testing.T) {
	initTestDB(t)

	id, err := InsertMemory("original content")
	if err != nil {
		t.Fatal(err)
	}

	if err := UpdateMemory(id, "updated content"); err != nil {
		t.Fatalf("UpdateMemory failed: %v", err)
	}

	rec, err := GetMemoryByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Content != "updated content" {
		t.Fatalf("expected 'updated content', got %q", rec.Content)
	}
}

func TestListMemories(t *testing.T) {
	initTestDB(t)

	InsertMemory("first memory")
	InsertMemory("second memory")
	InsertMemory("third memory")

	results, err := ListMemories(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 2 {
		t.Fatalf("expected at most 2 results, got %d", len(results))
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].Id == 0 {
		t.Fatal("expected non-zero id")
	}
	if results[0].Content == "" {
		t.Fatal("expected non-empty content")
	}
}

func TestCountMemories(t *testing.T) {
	initTestDB(t)

	InsertMemory("memory 1")
	InsertMemory("memory 2")

	count, err := CountMemories()
	if err != nil {
		t.Fatal(err)
	}
	if count < 2 {
		t.Fatalf("expected at least 2 memories, got %d", count)
	}
}
