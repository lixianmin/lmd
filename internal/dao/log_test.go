package dao

import (
	"testing"
)

func TestDocumentsLog_InsertComplete(t *testing.T) {
	initTestDB(t)

	id, err := InsertDocument("notes", "test.md", "Title", "body", 4, 1234567890, "hash1")
	if err != nil {
		t.Fatal(err)
	}
	if id <= 0 {
		t.Fatalf("expected positive id, got %d", id)
	}

	var logCount int
	DB.db.QueryRow("SELECT COUNT(*) FROM documents_log WHERE doc_id=? AND operation='INSERT'", id).Scan(&logCount)
	if logCount != 1 {
		t.Fatalf("expected 1 INSERT log, got %d", logCount)
	}

	err = UpdateFileModTime(id, 1234567890)
	if err != nil {
		t.Fatal(err)
	}

	logCount = 0
	DB.db.QueryRow("SELECT COUNT(*) FROM documents_log WHERE doc_id=? AND operation='UPDATE'", id).Scan(&logCount)
	if logCount != 1 {
		t.Fatalf("expected 1 UPDATE log after UpdateFileModTime, got %d", logCount)
	}
}

func TestDocumentsLog_Delete(t *testing.T) {
	initTestDB(t)

	docId, err := InsertDocument("notes", "test.md", "Title", "body", 4, 1234567890, "hash1")
	if err != nil {
		t.Fatal(err)
	}

	if err := DeleteDocument(docId); err != nil {
		t.Fatal(err)
	}

	var logCount int
	DB.db.QueryRow("SELECT COUNT(*) FROM documents_log WHERE doc_id=? AND operation='DELETE'", docId).Scan(&logCount)
	if logCount != 1 {
		t.Fatalf("expected 1 DELETE log, got %d", logCount)
	}
}

func TestChunksLog_Insert(t *testing.T) {
	initTestDB(t)

	docId, _ := InsertDocument("notes", "chunklog.md", "Title", "body", 4, 1234567890, "hash1")

	chunks := []ChunkData{{Content: "hello world", Position: 0, TokenCount: 2, Hash: "h1"}}
	tokenized := []string{"hello world"}
	records, err := InsertChunks(docId, chunks, tokenized)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(records))
	}

	var logCount int
	DB.db.QueryRow("SELECT COUNT(*) FROM chunks_log WHERE doc_id=? AND operation='INSERT'", docId).Scan(&logCount)
	if logCount != 1 {
		t.Fatalf("expected 1 INSERT chunk log, got %d", logCount)
	}
}

func TestChunksLog_DeleteByDocId(t *testing.T) {
	initTestDB(t)

	docId, _ := InsertDocument("notes", "chunkdel.md", "Title", "body", 4, 1234567890, "h1")

	chunks := []ChunkData{{Content: "hello", Position: 0, TokenCount: 1, Hash: "h1"}}
	tokenized := []string{"hello"}
	InsertChunks(docId, chunks, tokenized)

	if err := DeleteVectorsByDocId(docId); err != nil {
		t.Fatal(err)
	}

	var logCount int
	DB.db.QueryRow("SELECT COUNT(*) FROM chunks_log WHERE doc_id=? AND operation='DELETE'", docId).Scan(&logCount)
	if logCount != 1 {
		t.Fatalf("expected 1 DELETE chunk log, got %d", logCount)
	}
}

func TestDocumentsLog_UpsertInsert(t *testing.T) {
	initTestDB(t)

	doc := &DocumentRecord{
		Collection: "notes", Path: "upsert.md", Title: "Title",
		Body: "content", Hash: "h1", FileSize: 10, FileModTime: 123,
	}
	if err := UpsertDocument(doc); err != nil {
		t.Fatal(err)
	}

	var logCount int
	DB.db.QueryRow("SELECT COUNT(*) FROM documents_log WHERE doc_id=? AND operation='INSERT'", doc.Id).Scan(&logCount)
	if logCount != 1 {
		t.Fatalf("expected 1 INSERT log for upsert, got %d", logCount)
	}
}

func TestDocumentsLog_UpsertUpdate(t *testing.T) {
	initTestDB(t)

	doc := &DocumentRecord{
		Collection: "notes", Path: "upsert.md", Title: "Title",
		Body: "content", Hash: "h1", FileSize: 10, FileModTime: 123,
	}
	UpsertDocument(doc)

	doc.Title = "Updated"
	doc.Hash = "h2"
	if err := UpsertDocument(doc); err != nil {
		t.Fatal(err)
	}

	var insertCount, updateCount int
	DB.db.QueryRow("SELECT COUNT(*) FROM documents_log WHERE doc_id=? AND operation='INSERT'", doc.Id).Scan(&insertCount)
	DB.db.QueryRow("SELECT COUNT(*) FROM documents_log WHERE doc_id=? AND operation='UPDATE'", doc.Id).Scan(&updateCount)
	if insertCount != 1 {
		t.Fatalf("expected 1 INSERT log, got %d", insertCount)
	}
	if updateCount != 1 {
		t.Fatalf("expected 1 UPDATE log, got %d", updateCount)
	}
}

func TestDocumentsLog_RenameCollection(t *testing.T) {
	initTestDB(t)
	mustAddCollection(t, "oldname", "/tmp/old")

	docId, _ := InsertDocument("oldname", "file.md", "Title", "body", 4, 1234567890, "hash1")

	if err := RenameCollection("oldname", "newname"); err != nil {
		t.Fatal(err)
	}

	var logCount int
	DB.db.QueryRow("SELECT COUNT(*) FROM documents_log WHERE doc_id=? AND operation='UPDATE'", docId).Scan(&logCount)
	if logCount < 1 {
		t.Fatalf("expected UPDATE log after rename, got %d", logCount)
	}
}

