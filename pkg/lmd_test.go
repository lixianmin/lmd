package lmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	store, err := CreateStore(StoreOptions{
		DBPath: dbPath,
	})
	if err != nil {
		t.Fatalf("CreateStore failed: %v", err)
	}
	defer store.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestStoreCollectionWorkflow(t *testing.T) {
	dir := t.TempDir()
	testDir := filepath.Join(dir, "docs")
	os.MkdirAll(testDir, 0755)

	os.WriteFile(filepath.Join(testDir, "test.md"), []byte("# Hello\n\nWorld test content"), 0644)

	store, err := CreateStore(StoreOptions{
		DBPath: filepath.Join(dir, "test.sqlite"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	err = store.AddCollection("docs", CollectionConfig{
		Path:        testDir,
		GlobPattern: "*.md",
	})
	if err != nil {
		t.Fatal(err)
	}

	cols, err := store.ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].Name != "docs" {
		t.Fatalf("unexpected collections: %v", cols)
	}

	result, err := store.Update(context.Background(), UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Indexed != 1 {
		t.Fatalf("expected 1 indexed, got %d", result.Indexed)
	}

	results, err := store.SearchLex("Hello", LexOptions{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected search results")
	}

	doc, err := store.Get(results[0].Collection + "/" + results[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Hello" {
		t.Fatalf("expected title 'Hello', got '%s'", doc.Title)
	}
}
