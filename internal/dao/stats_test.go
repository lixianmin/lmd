package dao

import "testing"

func TestGetChunkCounts(t *testing.T) {
	initTestDB(t)

	total, embedded := GetChunkCounts()
	if total != 0 || embedded != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", total, embedded)
	}

	chunks := []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "h1"},
		{Content: "chunk two", Position: 1, TokenCount: 2, Hash: "h2"},
	}
	mustInsertDocWithChunks(t, "notes", "test.md", chunks)

	total, embedded = GetChunkCounts()
	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
	if embedded != 0 {
		t.Fatalf("expected embedded=0, got %d", embedded)
	}
}

func TestGetUnembeddedCount(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "a", Position: 0, TokenCount: 1, Hash: "h1"},
		{Content: "b", Position: 1, TokenCount: 1, Hash: "h2"},
	}
	_, records := mustInsertDocWithChunks(t, "notes", "test.md", chunks)

	if count := GetUnembeddedCount(); count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}

	vec := make([]float32, EmbeddingDim)
	InsertVector(records[0].Id, vec)

	if count := GetUnembeddedCount(); count != 1 {
		t.Fatalf("expected 1 after embedding one, got %d", count)
	}
}
