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

