package dao

import (
	"testing"
)

func TestUpsertDocumentInsert(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	doc := &DocumentRecord{
		Collection: "notes", Path: "hello.md", Title: "Hello",
		Body: "hello world", Hash: "abc123", FileSize: 11,
	}
	mustUpsertDoc(t, doc)

	if doc.Id <= 0 {
		t.Fatalf("expected positive id, got %d", doc.Id)
	}
	if doc.DocId == "" {
		t.Fatal("expected docId to be generated")
	}
}

func TestUpsertDocumentUpdate(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	doc := &DocumentRecord{
		Collection: "notes", Path: "hello.md", Title: "V1",
		Body: "version 1", Hash: "hash1", FileSize: 9,
	}
	mustUpsertDoc(t, doc)
	originalId := doc.Id

	doc.Body = "version 2"
	doc.Hash = "hash2"
	doc.Title = "V2"
	doc.FileSize = 9
	mustUpsertDoc(t, doc)

	if doc.Id != originalId {
		t.Fatalf("expected same id %d, got %d", originalId, doc.Id)
	}

	got, err := GetDocumentById(originalId)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "version 2" {
		t.Fatalf("expected updated body, got '%s'", got.Body)
	}
}

func TestGetDocumentByDocId(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	doc := &DocumentRecord{
		Collection: "notes", Path: "test.md", Title: "Test",
		Body: "content", Hash: "hashX", FileSize: 7,
	}
	mustUpsertDoc(t, doc)

	short := ShortDocId(doc.DocId)
	got, err := GetDocumentByDocId(short)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Test" {
		t.Fatalf("expected title 'Test', got '%s'", got.Title)
	}
}

func TestGetDocumentByDocIdNotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetDocumentByDocId("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent doc")
	}
}

func TestGetDocumentByPath(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	mustUpsertDoc(t, &DocumentRecord{
		Collection: "notes", Path: "deep/test.md", Title: "Deep",
		Body: "deep content", Hash: "hashD", FileSize: 12,
	})

	got, err := GetDocumentByPath("notes", "deep/test.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "Deep" {
		t.Fatalf("expected 'Deep', got '%s'", got.Title)
	}
}

func TestGetDocumentById(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	doc := &DocumentRecord{
		Collection: "notes", Path: "x.md", Title: "X",
		Body: "body", Hash: "hashX1", FileSize: 4,
	}
	mustUpsertDoc(t, doc)

	got, err := GetDocumentById(doc.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got.DocId != doc.DocId {
		t.Fatalf("expected docId %s, got %s", doc.DocId, got.DocId)
	}
}

func TestDeleteDocument(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	doc := &DocumentRecord{
		Collection: "notes", Path: "del.md", Title: "Del",
		Body: "delete me", Hash: "hashDel", FileSize: 9,
	}
	mustUpsertDoc(t, doc)

	if err := DeleteDocument(doc.Id); err != nil {
		t.Fatal(err)
	}

	_, err := GetDocumentById(doc.Id)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestDeleteDocumentCleansUpChunks(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	doc := &DocumentRecord{
		Collection: "notes", Path: "del.md", Title: "Del",
		Body: "delete me", Hash: "hashDel", FileSize: 9,
	}
	mustUpsertDoc(t, doc)

	chunks := []ChunkData{
		{Content: "chunk one", Position: 0, TokenCount: 2, Hash: "h1"},
		{Content: "chunk two", Position: 1, TokenCount: 2, Hash: "h2"},
	}
	var tokenized []string
	for _, c := range chunks {
		tokenized = append(tokenized, c.Content)
	}
	_, err := InsertChunks(doc.Id, chunks, tokenized)
	if err != nil {
		t.Fatal(err)
	}

	vec := make([]float32, EmbeddingDim)
	for i := range vec {
		vec[i] = 0.1
	}
	chunkList, _ := GetChunksByDocId(doc.Id)
	chunkId := chunkList[0].Id
	if err := InsertVector(chunkId, doc.Id, "notes", vec); err != nil {
		t.Fatal(err)
	}

	if err := DeleteDocument(doc.Id); err != nil {
		t.Fatal(err)
	}

	gotChunks, _ := GetChunksByDocId(doc.Id)
	if len(gotChunks) != 0 {
		t.Fatalf("expected 0 chunks after document delete, got %d", len(gotChunks))
	}

	_, err = GetDocumentById(doc.Id)
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestListDocumentsByCollection(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "a", "/a")
	mustAddCollection(t, "b", "/b")

	mustUpsertDoc(t, &DocumentRecord{Collection: "a", Path: "1.md", Title: "A1", Body: "a1", Hash: "h1", FileSize: 2})
	mustUpsertDoc(t, &DocumentRecord{Collection: "a", Path: "2.md", Title: "A2", Body: "a2", Hash: "h2", FileSize: 2})
	mustUpsertDoc(t, &DocumentRecord{Collection: "b", Path: "3.md", Title: "B1", Body: "b1", Hash: "h3", FileSize: 2})

	docs, err := ListDocumentsByCollection("a")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 {
		t.Fatalf("expected 2 docs in collection 'a', got %d", len(docs))
	}
}

func TestCountDocuments(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	count, _ := CountDocuments()
	if count != 0 {
		t.Fatalf("expected 0, got %d", count)
	}

	mustUpsertDoc(t, &DocumentRecord{Collection: "notes", Path: "1.md", Title: "1", Body: "b", Hash: "h1", FileSize: 1})
	mustUpsertDoc(t, &DocumentRecord{Collection: "notes", Path: "2.md", Title: "2", Body: "b", Hash: "h2", FileSize: 1})

	count, _ = CountDocuments()
	if count != 2 {
		t.Fatalf("expected 2, got %d", count)
	}
}

func TestGetDocumentHash(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	mustUpsertDoc(t, &DocumentRecord{
		Collection: "notes", Path: "hash.md", Title: "H",
		Body: "body", Hash: "myhash", FileSize: 4,
	})

	hash, err := GetDocumentHash("notes", "hash.md")
	if err != nil {
		t.Fatal(err)
	}
	if hash != "myhash" {
		t.Fatalf("expected 'myhash', got '%s'", hash)
	}
}

func TestGetDocumentHashNotFound(t *testing.T) {
	initTestDB(t)

	_, err := GetDocumentHash("x", "y.md")
	if err == nil {
		t.Fatal("expected error for nonexistent document")
	}
}

func TestShortDocId(t *testing.T) {
	if ShortDocId("abcdef1234567890") != "abcdef12" {
		t.Fatal("expected first 8 chars")
	}
	if ShortDocId("abc") != "abc" {
		t.Fatal("expected original for short string")
	}
}

func TestGenerateDocId(t *testing.T) {
	id1 := generateDocId("col", "path", "hash")
	id2 := generateDocId("col", "path", "hash")
	id3 := generateDocId("col", "other", "hash")

	if id1 != id2 {
		t.Fatal("same inputs should produce same docId")
	}
	if id1 == id3 {
		t.Fatal("different inputs should produce different docId")
	}
}

func TestInsertDocument(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	docId, err := InsertDocument("notes", "test.md", "Test Title", "body content", 12, 1234567890, "hash123")
	if err != nil {
		t.Fatal(err)
	}
	if docId <= 0 {
		t.Fatalf("expected positive docId, got %d", docId)
	}

	doc, err := GetDocumentById(docId)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Hash != "hash123" {
		t.Fatalf("expected hash 'hash123', got '%s'", doc.Hash)
	}
	if doc.FileModTime != 1234567890 {
		t.Fatalf("expected file_mod_time=1234567890, got %d", doc.FileModTime)
	}
	if doc.Collection != "notes" || doc.Path != "test.md" {
		t.Fatalf("unexpected collection/path: %s/%s", doc.Collection, doc.Path)
	}
}

func TestUpdateFileModTime(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "notes", "/data")

	docId, _ := InsertDocument("notes", "test.md", "Title", "body", 4, 0, "hash1")

	err := UpdateFileModTime(docId, 1234567890)
	if err != nil {
		t.Fatal(err)
	}

	doc, _ := GetDocumentById(docId)
	if doc.FileModTime != 1234567890 {
		t.Fatalf("expected file_mod_time=1234567890, got %d", doc.FileModTime)
	}
}


