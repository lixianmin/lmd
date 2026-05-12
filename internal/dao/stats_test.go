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

func TestGetHydeCounts(t *testing.T) {
	initTestDB(t)

	total, done := GetHydeCounts()
	if total != 0 || done != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", total, done)
	}

	chunks := []ChunkData{
		{Content: "hello", Position: 0, TokenCount: 1, Hash: "h1"},
	}
	mustInsertDocWithChunks(t, "notes", "a.md", chunks)

	doc2 := &DocumentRecord{
		Collection: "notes", Path: "b.md", Title: "b.md",
		Body: "body", Hash: "hash_b", FileSize: 4,
	}
	mustUpsertDoc(t, doc2)

	total, done = GetHydeCounts()
	if total != 2 {
		t.Fatalf("expected total=2, got %d", total)
	}
	if done != 0 {
		t.Fatalf("expected done=0, got %d", done)
	}

	mustInsertDocWithChunks(t, "@hyde", "a.md", chunks)
	total, done = GetHydeCounts()
	if total != 2 {
		t.Fatalf("expected total=2 after hyde, got %d", total)
	}
	if done != 1 {
		t.Fatalf("expected done=1 after hyde, got %d", done)
	}
}

