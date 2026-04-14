package dao

import (
	"testing"
)

func TestAddCollection(t *testing.T) {
	initTestDB(t)

	err := AddCollection("notes", "/data/notes", "**/*.md", nil)
	if err != nil {
		t.Fatal(err)
	}

	cols, err := ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 1 || cols[0].Name != "notes" {
		t.Fatalf("expected 1 collection 'notes', got %v", cols)
	}
	if cols[0].Path != "/data/notes" {
		t.Fatalf("expected path '/data/notes', got '%s'", cols[0].Path)
	}
}

func TestAddCollectionWithIgnorePatterns(t *testing.T) {
	initTestDB(t)

	err := AddCollection("notes", "/data/notes", "*.md", []string{"*.tmp", ".git"})
	if err != nil {
		t.Fatal(err)
	}

	cols, err := ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(cols[0].IgnorePatterns) != 2 {
		t.Fatalf("expected 2 ignore patterns, got %v", cols[0].IgnorePatterns)
	}
}

func TestAddCollectionDuplicate(t *testing.T) {
	initTestDB(t)

	if err := AddCollection("notes", "/a", "*.md", nil); err != nil {
		t.Fatal(err)
	}
	err := AddCollection("notes", "/b", "*.md", nil)
	if err == nil {
		t.Fatal("expected error for duplicate collection name")
	}
}

func TestRemoveCollection(t *testing.T) {
	initTestDB(t)

	mustAddCollection(t, "notes", "/data/notes")

	if err := RemoveCollection("notes"); err != nil {
		t.Fatal(err)
	}

	cols, _ := ListCollections()
	if len(cols) != 0 {
		t.Fatalf("expected 0 collections, got %d", len(cols))
	}
}

func TestRemoveCollectionNotFound(t *testing.T) {
	initTestDB(t)

	err := RemoveCollection("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent collection")
	}
}

func TestListCollectionsMultiple(t *testing.T) {
	initTestDB(t)

	mustAddCollection(t, "alpha", "/a")
	mustAddCollection(t, "beta", "/b")

	cols, err := ListCollections()
	if err != nil {
		t.Fatal(err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(cols))
	}
	if cols[0].Name != "alpha" || cols[1].Name != "beta" {
		t.Fatalf("expected sorted order alpha/beta, got %v", cols)
	}
}

func TestListCollectionsDocCount(t *testing.T) {
	initTestDB(t)

	mustAddCollection(t, "notes", "/data/notes")

	mustUpsertDoc(t, &DocumentRecord{
		Collection: "notes", Path: "a.md", Title: "A",
		Body: "body a", Hash: "hash_a", FileSize: 10,
	})
	mustUpsertDoc(t, &DocumentRecord{
		Collection: "notes", Path: "b.md", Title: "B",
		Body: "body b", Hash: "hash_b", FileSize: 20,
	})

	cols, _ := ListCollections()
	if cols[0].DocCount != 2 {
		t.Fatalf("expected doc_count=2, got %d", cols[0].DocCount)
	}
}

func TestRenameCollection(t *testing.T) {
	initTestDB(t)

	mustAddCollection(t, "old", "/data")

	if err := RenameCollection("old", "new"); err != nil {
		t.Fatal(err)
	}

	cols, _ := ListCollections()
	if len(cols) != 1 || cols[0].Name != "new" {
		t.Fatalf("expected collection 'new', got %v", cols)
	}
}

func TestRenameCollectionNotFound(t *testing.T) {
	initTestDB(t)

	err := RenameCollection("nonexistent", "new")
	if err == nil {
		t.Fatal("expected error for nonexistent collection")
	}
}
