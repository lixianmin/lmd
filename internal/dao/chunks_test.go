package dao

import (
	"testing"
)

func mustInsertDocWithChunks(t *testing.T, collection, path string, chunks []ChunkData) (*DocumentRecord, []ChunkRecord) {
	t.Helper()
	mustAddCollection(t, collection, collection)
	doc := &DocumentRecord{
		Collection: collection, Path: path, Title: path,
		Body: "body", Hash: "hash_" + path, FileSize: 4,
	}
	mustUpsertDoc(t, doc)

	var tokenized []string
	for _, c := range chunks {
		tokenized = append(tokenized, c.Content)
	}
	records, err := InsertChunks(doc.Id, chunks, tokenized)
	if err != nil {
		t.Fatalf("InsertChunks: %v", err)
	}
	return doc, records
}

func TestInsertChunks(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "hello world", Position: 0, TokenCount: 2, Hash: "h1"},
		{Content: "goodbye world", Position: 1, TokenCount: 2, Hash: "h2"},
	}
	_, records := mustInsertDocWithChunks(t, "notes", "test.md", chunks)

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Content != "hello world" {
		t.Fatalf("expected 'hello world', got '%s'", records[0].Content)
	}
	if records[1].Seq != 1 {
		t.Fatalf("expected seq=1, got %d", records[1].Seq)
	}
}

func TestInsertChunksMismatchedLength(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/notes")

	doc := &DocumentRecord{
		Collection: "notes", Path: "test.md", Title: "T",
		Body: "body", Hash: "h", FileSize: 4,
	}
	mustUpsertDoc(t, doc)

	_, err := InsertChunks(doc.Id, []ChunkData{{Content: "a"}}, []string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for mismatched lengths")
	}
}

func TestSearchFTS(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "Go is a programming language", Position: 0, TokenCount: 5, Hash: "h1"},
		{Content: "Rust is also a programming language", Position: 1, TokenCount: 6, Hash: "h2"},
	}
	mustInsertDocWithChunks(t, "notes", "lang.md", chunks)

	results, err := SearchFTS("programming", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 FTS results, got %d", len(results))
	}
}

func TestSearchFTSByCollection(t *testing.T) {
	initTestDB(t)

	chunks1 := []ChunkData{{Content: "machine learning algorithms", Position: 0, TokenCount: 3, Hash: "h1"}}
	chunks2 := []ChunkData{{Content: "machine learning models", Position: 0, TokenCount: 3, Hash: "h2"}}
	mustInsertDocWithChunks(t, "ai", "ml.md", chunks1)
	mustInsertDocWithChunks(t, "math", "ml.md", chunks2)

	results, err := SearchFTS("machine", "ai", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result for collection 'ai', got %d", len(results))
	}
}

func TestInsertVectorAndGetUnembedded(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "h1"},
		{Content: "chunk two", Position: 1, TokenCount: 2, Hash: "h2"},
	}
	_, records := mustInsertDocWithChunks(t, "notes", "test.md", chunks)

	unembedded, err := GetUnembeddedChunks(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(unembedded) != 2 {
		t.Fatalf("expected 2 unembedded, got %d", len(unembedded))
	}

	vec := make([]float32, EmbeddingDim)
	for i := range vec {
		vec[i] = 0.1
	}
	if err := InsertVector(records[0].Id, vec); err != nil {
		t.Fatal(err)
	}

	unembedded, _ = GetUnembeddedChunks(0)
	if len(unembedded) != 1 {
		t.Fatalf("expected 1 unembedded after insert, got %d", len(unembedded))
	}

	unembedded, _ = GetUnembeddedChunks(1)
	if len(unembedded) != 1 {
		t.Fatalf("expected 1 with limit=1, got %d", len(unembedded))
	}
}

func TestQueryVectors(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "vector test", Position: 0, TokenCount: 2, Hash: "h1"},
	}
	_, records := mustInsertDocWithChunks(t, "notes", "vec.md", chunks)

	vec := make([]float32, EmbeddingDim)
	for i := range vec {
		vec[i] = float32(i%10) * 0.1
	}
	if err := InsertVector(records[0].Id, vec); err != nil {
		t.Fatal(err)
	}

	results, err := QueryVectors(vec, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 vector result, got %d", len(results))
	}
	if results[0].ChunkId != records[0].Id {
		t.Fatalf("expected chunkID %d, got %d", records[0].Id, results[0].ChunkId)
	}
}

func TestGetChunksByDocId(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "aaa", Position: 0, TokenCount: 1, Hash: "h1"},
		{Content: "bbb", Position: 1, TokenCount: 1, Hash: "h2"},
	}
	doc, _ := mustInsertDocWithChunks(t, "notes", "chunks.md", chunks)

	got, err := GetChunksByDocId(doc.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(got))
	}
}

func TestGetChunkById(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{{Content: "single", Position: 0, TokenCount: 1, Hash: "h1"}}
	_, records := mustInsertDocWithChunks(t, "notes", "one.md", chunks)

	got, err := GetChunkById(records[0].Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "single" {
		t.Fatalf("expected 'single', got '%s'", got.Content)
	}
}

func TestGetChunkByIdNotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetChunkById(99999)
	if err == nil {
		t.Fatal("expected error for nonexistent chunk")
	}
}

func TestDeleteVectorsByDocId(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "to delete", Position: 0, TokenCount: 2, Hash: "h1"},
	}
	doc, _ := mustInsertDocWithChunks(t, "notes", "del.md", chunks)

	vec := make([]float32, EmbeddingDim)
	InsertVector(doc.Id, vec)

	if err := DeleteVectorsByDocId(doc.Id); err != nil {
		t.Fatal(err)
	}

	got, _ := GetChunksByDocId(doc.Id)
	if len(got) != 0 {
		t.Fatalf("expected 0 chunks after delete, got %d", len(got))
	}
}

func TestPadVector(t *testing.T) {
	short := []float32{1.0, 2.0}
	padded := padVector(short)
	if len(padded) != EmbeddingDim {
		t.Fatalf("expected len %d, got %d", EmbeddingDim, len(padded))
	}
	if padded[0] != 1.0 || padded[1] != 2.0 || padded[2] != 0.0 {
		t.Fatal("padding incorrect")
	}

	exact := make([]float32, EmbeddingDim)
	if p := padVector(exact); len(p) != EmbeddingDim {
		t.Fatalf("expected no padding needed")
	}
}

func TestSimilarityToScore(t *testing.T) {
	score := SimilarityToScore(0.3)
	if score != 0.7 {
		t.Fatalf("expected 0.7, got %f", score)
	}
}

func TestGetEmbeddingsByChunkIds(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "test content", Position: 0, TokenCount: 2, Hash: "h1"},
	}
	_, records := mustInsertDocWithChunks(t, "notes", "emb_test.md", chunks)
	chunkId := records[0].Id

	vec := make([]float32, EmbeddingDim)
	for i := range vec {
		vec[i] = float32(i)
	}

	if err := InsertVector(chunkId, vec); err != nil {
		t.Fatal(err)
	}

	results, err := GetEmbeddingsByChunkIds([]int64{chunkId})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ChunkID != chunkId {
		t.Fatalf("expected chunkId %d, got %d", chunkId, results[0].ChunkID)
	}
	if len(results[0].Embedding) != EmbeddingDim {
		t.Fatalf("expected dim %d, got %d", EmbeddingDim, len(results[0].Embedding))
	}
}

func TestGetEmbeddingsByChunkIdsEmpty(t *testing.T) {
	initTestDB(t)

	results, err := GetEmbeddingsByChunkIds(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for empty input, got %d", len(results))
	}
}

func TestInsertChunksIgnoreDuplicate(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "h1"},
		{Content: "chunk two", Position: 1, TokenCount: 2, Hash: "h2"},
	}
	doc, records1 := mustInsertDocWithChunks(t, "notes", "dup.md", chunks)

	if len(records1) != 2 {
		t.Fatalf("first insert: expected 2 records, got %d", len(records1))
	}

	var tokenized []string
	for _, c := range chunks {
		tokenized = append(tokenized, c.Content)
	}
	records2, err := InsertChunks(doc.Id, chunks, tokenized)
	if err != nil {
		t.Fatalf("InsertChunks on duplicate should not error: %v", err)
	}
	// INSERT OR IGNORE 跳过冲突行，返回的 records 不应包含跳过的行
	if len(records2) != 0 {
		t.Fatalf("duplicate insert: expected 0 records (all ignored), got %d", len(records2))
	}

	got, err := GetChunksByDocId(doc.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected still 2 chunks after duplicate insert, got %d", len(got))
	}
}
