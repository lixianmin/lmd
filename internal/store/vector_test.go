package store

import (
	"testing"
)

func makeTestVec(vals ...float32) []float32 {
	vec := make([]float32, EmbeddingDim)
	for i, v := range vals {
		if i < EmbeddingDim {
			vec[i] = v
		}
	}
	return vec
}

func TestStoreAndQueryVectors(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	doc := &DocumentRecord{Collection: "test", Path: "a.md", Title: "Test", Body: "hello", Hash: "h1"}
	_ = UpsertDocument(db, doc)

	chunks, err := InsertChunks(db, doc.ID, []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "c1"},
		{Content: "chunk two", Position: 10, TokenCount: 2, Hash: "c2"},
	}, []string{"chunk one", "chunk two"})
	if err != nil {
		t.Fatalf("InsertChunks failed: %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	vec1 := makeTestVec(0.9, 0.1)
	vec2 := makeTestVec(0.1, 0.9)

	err = InsertVector(db, chunks[0].ID, vec1)
	if err != nil {
		t.Fatalf("InsertVector failed: %v", err)
	}
	err = InsertVector(db, chunks[1].ID, vec2)
	if err != nil {
		t.Fatalf("InsertVector failed: %v", err)
	}

	query := makeTestVec(0.9, 0.1)
	results, err := QueryVectors(db, query, 2)
	if err != nil {
		t.Fatalf("QueryVectors failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected vector search results")
	}
	if results[0].ChunkID != chunks[0].ID {
		t.Fatalf("expected closest match to be chunk 0 (id=%d), got chunk %d", chunks[0].ID, results[0].ChunkID)
	}
}

func TestGetUnembeddedChunks(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	doc := &DocumentRecord{Collection: "test", Path: "a.md", Title: "Test", Body: "hello", Hash: "h1"}
	_ = UpsertDocument(db, doc)

	_, _ = InsertChunks(db, doc.ID, []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "c1"},
		{Content: "chunk two", Position: 10, TokenCount: 2, Hash: "c2"},
	}, []string{"chunk one", "chunk two"})

	unembedded, err := GetUnembeddedChunks(db)
	if err != nil {
		t.Fatalf("GetUnembeddedChunks failed: %v", err)
	}
	if len(unembedded) != 2 {
		t.Fatalf("expected 2 unembedded chunks, got %d", len(unembedded))
	}

	_ = InsertVector(db, unembedded[0].ID, makeTestVec(0.1))

	unembedded2, _ := GetUnembeddedChunks(db)
	if len(unembedded2) != 1 {
		t.Fatalf("expected 1 unembedded after embedding, got %d", len(unembedded2))
	}
}

func TestGetChunkByID(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	doc := &DocumentRecord{Collection: "test", Path: "a.md", Title: "Test", Body: "hello", Hash: "h1"}
	_ = UpsertDocument(db, doc)

	chunks, _ := InsertChunks(db, doc.ID, []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "c1"},
	}, []string{"chunk one"})

	got, err := GetChunkByID(db, chunks[0].ID)
	if err != nil {
		t.Fatalf("GetChunkByID failed: %v", err)
	}
	if got.Content != "chunk one" {
		t.Fatalf("expected content 'chunk one', got %q", got.Content)
	}
}

func TestDeleteVectorsByDocId(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	doc := &DocumentRecord{Collection: "test", Path: "a.md", Title: "Test", Body: "hello", Hash: "h1"}
	_ = UpsertDocument(db, doc)

	chunks, _ := InsertChunks(db, doc.ID, []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "c1"},
	}, []string{"chunk one"})
	_ = InsertVector(db, chunks[0].ID, makeTestVec(0.1))

	err := DeleteVectorsByDocId(db, doc.ID)
	if err != nil {
		t.Fatalf("DeleteVectorsByDocId failed: %v", err)
	}

	_, err = GetChunkByID(db, chunks[0].ID)
	if err == nil {
		t.Fatal("expected chunk to be deleted")
	}
}
