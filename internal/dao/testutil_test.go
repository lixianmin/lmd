package dao

import (
	"path/filepath"
	"testing"
)

func initTestDB(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")
	if err := Init(dbPath); err != nil {
		t.Fatalf("Init failed: %v", err)
	}
	t.Cleanup(func() {
		DB.Close()
		DB = nil
	})
}

func mustAddCollection(t *testing.T, name, path string) {
	t.Helper()
	if err := AddCollection(name, path, "**/*.md", nil); err != nil {
		t.Fatalf("AddCollection(%q): %v", name, err)
	}
}

func mustUpsertDoc(t *testing.T, doc *DocumentRecord) {
	t.Helper()
	if err := UpsertDocument(doc); err != nil {
		t.Fatalf("UpsertDocument(%+v): %v", doc, err)
	}
}
