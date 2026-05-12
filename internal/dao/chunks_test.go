package dao

import (
	"fmt"
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

func TestQueryVectors(t *testing.T) {
	initTestDB(t)

	chunks := []ChunkData{
		{Content: "vector test", Position: 0, TokenCount: 2, Hash: "h1"},
	}
	doc, records := mustInsertDocWithChunks(t, "notes", "vec.md", chunks)

	vec := make([]float32, EmbeddingDim)
	for i := range vec {
		vec[i] = float32(i%10) * 0.1
	}
	if err := InsertVector(records[0].Id, doc.Id, "notes", vec); err != nil {
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

func TestQueryVectorsByDocIds(t *testing.T) {
	initTestDB(t)

	chunks1 := []ChunkData{
		{Content: "docker container", Position: 0, TokenCount: 2, Hash: "h1"},
	}
	doc1, rec1 := mustInsertDocWithChunks(t, "qvbi1", "a.md", chunks1)

	chunks2 := []ChunkData{
		{Content: "kubernetes pod", Position: 0, TokenCount: 2, Hash: "h2"},
	}
	doc2, rec2 := mustInsertDocWithChunks(t, "qvbi2", "b.md", chunks2)

	vec1 := make([]float32, EmbeddingDim)
	for i := range vec1 {
		vec1[i] = 0.1
	}
	vec2 := make([]float32, EmbeddingDim)
	for i := range vec2 {
		vec2[i] = 0.9
	}
	InsertVector(rec1[0].Id, doc1.Id, "qvbi1", vec1)
	InsertVector(rec2[0].Id, doc2.Id, "qvbi2", vec2)

	query := make([]float32, EmbeddingDim)
	for i := range query {
		query[i] = 0.1
	}

	results, err := QueryVectorsByDocIds(query, []int64{doc1.Id}, 10)
	if err != nil {
		t.Fatalf("QueryVectorsByDocIds: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result filtered by doc1, got %d", len(results))
	}
	if results[0].ChunkId != rec1[0].Id {
		t.Fatalf("expected chunk from doc1, got chunkId %d", results[0].ChunkId)
	}

	results2, err := QueryVectorsByDocIds(query, []int64{doc2.Id}, 10)
	if err != nil {
		t.Fatalf("QueryVectorsByDocIds(doc2): %v", err)
	}
	if len(results2) != 1 {
		t.Fatalf("expected 1 result for doc2 (within top-10), got %d", len(results2))
	}

	allResults, err := QueryVectorsByDocIds(query, []int64{doc1.Id, doc2.Id}, 10)
	if err != nil {
		t.Fatalf("QueryVectorsByDocIds(both): %v", err)
	}
	if len(allResults) != 2 {
		t.Fatalf("expected 2 results for both docs, got %d", len(allResults))
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
	doc, records := mustInsertDocWithChunks(t, "notes", "del.md", chunks)

	vec := make([]float32, EmbeddingDim)
	InsertVector(records[0].Id, doc.Id, "notes", vec)

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
	doc, records := mustInsertDocWithChunks(t, "notes", "emb_test.md", chunks)
	chunkId := records[0].Id

	vec := make([]float32, EmbeddingDim)
	for i := range vec {
		vec[i] = float32(i)
	}

	if err := InsertVector(chunkId, doc.Id, "notes", vec); err != nil {
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

func TestInsertChunksAndVectors(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	docId, err := InsertDocument("notes", "test.md", "Title", "body", 4, "h1")
	if err != nil {
		t.Fatal(err)
	}

	chunks := []ChunkData{
		{Content: "hello world", Position: 0, TokenCount: 2, Hash: "h1"},
		{Content: "foo bar", Position: 1, TokenCount: 2, Hash: "h1"},
	}
	tokenized := []string{"hello world", "foo bar"}
	vecs := make([][]float32, 2)
	vecs[0] = make([]float32, EmbeddingDim)
	vecs[1] = make([]float32, EmbeddingDim)
	vecs[0][0] = 0.5
	vecs[1][0] = 0.8

	inserted, err := InsertChunksAndVectors(docId, "notes", 0, chunks, tokenized, vecs)
	if err != nil {
		t.Fatal(err)
	}
	if len(inserted) != 2 {
		t.Fatalf("expected 2 inserted chunks, got %d", len(inserted))
	}

	for _, c := range inserted {
		var vecCount int
		rows, _ := WithQuery("SELECT COUNT(*) FROM chunks_vec WHERE chunk_id=?", c.Id)
		if rows.Next() {
			rows.Scan(&vecCount)
		}
		rows.Close()
		if vecCount != 1 {
			t.Fatalf("chunk %d: expected 1 vector, got %d", c.Id, vecCount)
		}
	}

	ftsResults, _ := SearchFTS("hello", "notes", 10)
	if len(ftsResults) != 1 {
		t.Fatalf("expected 1 FTS result for 'hello', got %d", len(ftsResults))
	}
}

func TestInsertChunksAndVectorsMismatch(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")
	docId, _ := InsertDocument("notes", "test.md", "Title", "body", 4, "h1")

	chunks := []ChunkData{{Content: "a", Position: 0, TokenCount: 1, Hash: "h"}}
	tokenized := []string{"a"}
	vecs := make([][]float32, 2)

	_, err := InsertChunksAndVectors(docId, "notes", 0, chunks, tokenized, vecs)
	if err == nil {
		t.Fatal("expected error for mismatched lengths")
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

func TestInsertChunksAndVectorsBatchSeq(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	docId, err := InsertDocument("notes", "big.md", "Big Doc", "body", 4, "h1")
	if err != nil {
		t.Fatal(err)
	}

	totalChunks := 25
	allChunks := make([]ChunkData, totalChunks)
	allTokenized := make([]string, totalChunks)
	allVecs := make([][]float32, totalChunks)
	for i := 0; i < totalChunks; i++ {
		allChunks[i] = ChunkData{Content: fmt.Sprintf("chunk %d", i), Position: i, TokenCount: 1, Hash: "h1"}
		allTokenized[i] = allChunks[i].Content
		allVecs[i] = make([]float32, EmbeddingDim)
		allVecs[i][0] = float32(i)
	}

	batch0 := allChunks[:20]
	tok0 := allTokenized[:20]
	vec0 := allVecs[:20]
	records0, err := InsertChunksAndVectors(docId, "notes", 0, batch0, tok0, vec0)
	if err != nil {
		t.Fatalf("batch 0: %v", err)
	}
	if len(records0) != 20 {
		t.Fatalf("batch 0: expected 20 records, got %d", len(records0))
	}

	batch1 := allChunks[20:]
	tok1 := allTokenized[20:]
	vec1 := allVecs[20:]
	records1, err := InsertChunksAndVectors(docId, "notes", 20, batch1, tok1, vec1)
	if err != nil {
		t.Fatalf("batch 1: %v", err)
	}
	if len(records1) != 5 {
		t.Fatalf("batch 1: expected 5 records, got %d", len(records1))
	}

	all, err := GetChunksByDocId(docId)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 25 {
		t.Fatalf("expected 25 total chunks, got %d", len(all))
	}
	for i, c := range all {
		if c.Seq != i {
			t.Fatalf("chunk %d: expected seq=%d, got seq=%d", i, i, c.Seq)
		}
	}
}
