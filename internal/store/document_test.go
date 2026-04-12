package store

import (
	"testing"
)

func TestUpsertDocument(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "Test Document",
		Body:       "Hello world",
		Hash:       "abc123",
		FileSize:   100,
	}
	err := UpsertDocument(db, &doc, "hello world", "test document")
	if err != nil {
		t.Fatalf("UpsertDocument failed: %v", err)
	}

	if doc.DocID == "" {
		t.Fatal("docid should be set")
	}
	if doc.ID == 0 {
		t.Fatal("id should be set")
	}
}

func TestUpsertDocumentUpdate(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc1 := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "V1",
		Body:       "body v1",
		Hash:       "hash1",
		FileSize:   10,
	}
	_ = UpsertDocument(db, &doc1, "body v1", "v1")

	doc2 := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "V2",
		Body:       "body v2",
		Hash:       "hash2",
		FileSize:   20,
	}
	_ = UpsertDocument(db, &doc2, "body v2", "v2")

	docs, _ := ListDocumentsByCollection(db, "notes")
	if len(docs) != 1 {
		t.Fatalf("expected 1 document (updated), got %d", len(docs))
	}
	if docs[0].Title != "V2" {
		t.Fatalf("expected title V2, got %s", docs[0].Title)
	}
}

func TestGetDocumentByDocID(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "Test",
		Body:       "content",
		Hash:       "hash1",
	}
	_ = UpsertDocument(db, &doc, "content", "test")

	got, err := GetDocumentByDocID(db, doc.DocID)
	if err != nil {
		t.Fatalf("GetDocumentByDocID failed: %v", err)
	}
	if got.Path != "test.md" {
		t.Fatalf("expected path test.md, got %s", got.Path)
	}
}

func TestGetDocumentByPath(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "sub/test.md",
		Title:      "Test",
		Body:       "content",
		Hash:       "hash1",
	}
	_ = UpsertDocument(db, &doc, "content", "test")

	got, err := GetDocumentByPath(db, "notes", "sub/test.md")
	if err != nil {
		t.Fatalf("GetDocumentByPath failed: %v", err)
	}
	if got.DocID != doc.DocID {
		t.Fatalf("expected docid %s, got %s", doc.DocID, got.DocID)
	}
}

func TestDeleteDocument(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "Test",
		Body:       "content",
		Hash:       "hash1",
	}
	_ = UpsertDocument(db, &doc, "content", "test")

	if err := DeleteDocument(db, doc.ID); err != nil {
		t.Fatalf("DeleteDocument failed: %v", err)
	}

	_, err := GetDocumentByDocID(db, doc.DocID)
	if err == nil {
		t.Fatal("expected error for deleted document")
	}
}

func TestGetDocumentHashByPath(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	doc := DocumentRecord{
		Collection: "notes",
		Path:       "test.md",
		Title:      "Test",
		Body:       "content",
		Hash:       "hash1",
	}
	_ = UpsertDocument(db, &doc, "content", "test")

	hash, err := GetDocumentHash(db, "notes", "test.md")
	if err != nil {
		t.Fatalf("GetDocumentHash failed: %v", err)
	}
	if hash != "hash1" {
		t.Fatalf("expected hash hash1, got %s", hash)
	}
}

func TestSearchFTS(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)

	docs := []struct {
		path   string
		title  string
		tokens string
	}{
		{"go.md", "Go Language", "go golang 并发 编程 语言"},
		{"python.md", "Python Notes", "python 编程 数据 科学"},
		{"rust.md", "Rust Guide", "rust 系统 编程 安全 内存"},
	}
	for _, d := range docs {
		doc := DocumentRecord{Collection: "notes", Path: d.path, Title: d.title, Body: "body", Hash: d.path}
		_ = UpsertDocument(db, &doc, d.tokens, d.title)
	}

	results, err := SearchFTS(db, "编程 语言", "", 10)
	if err != nil {
		t.Fatalf("SearchFTS failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}

	paths := map[string]bool{}
	for _, r := range results {
		paths[r.Path] = true
	}
	if !paths["go.md"] {
		t.Fatal("expected go.md in results for '编程 语言'")
	}
}

func TestSearchFTSWithCollection(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/notes", "**/*.md", nil)
	_ = AddCollection(db, "docs", "/docs", "**/*.md", nil)

	doc1 := DocumentRecord{Collection: "notes", Path: "test.md", Title: "搜索测试", Body: "body", Hash: "h1"}
	_ = UpsertDocument(db, &doc1, "搜索 测试 中文", "搜索 测试")

	doc2 := DocumentRecord{Collection: "docs", Path: "test.md", Title: "搜索文档", Body: "body", Hash: "h2"}
	_ = UpsertDocument(db, &doc2, "搜索 文档", "搜索 文档")

	results, _ := SearchFTS(db, "搜索", "notes", 10)
	for _, r := range results {
		if r.Collection != "notes" {
			t.Fatalf("expected only notes collection, got %s", r.Collection)
		}
	}
}
