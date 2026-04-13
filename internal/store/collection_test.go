package store

import (
	"database/sql"
	"testing"
)

func TestAddCollection(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	err := AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	if err != nil {
		t.Fatalf("AddCollection failed: %v", err)
	}

	name, path, glob := getCollection(t, db, "notes")
	if name != "notes" || path != "/home/user/notes" || glob != "**/*.md" {
		t.Fatalf("unexpected values: name=%s path=%s glob=%s", name, path, glob)
	}
}

func TestAddCollectionDuplicate(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	err := AddCollection(db, "notes", "/home/user/other", "**/*.md", nil)
	if err == nil {
		t.Fatal("expected error for duplicate collection name")
	}
}

func TestRemoveCollection(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	if err := RemoveCollection(db, "notes"); err != nil {
		t.Fatalf("RemoveCollection failed: %v", err)
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM collections WHERE name='notes'").Scan(&count)
	if count != 0 {
		t.Fatal("collection should be removed")
	}
}

func TestRemoveCollectionNotFound(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	err := RemoveCollection(db, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent collection")
	}
}

func TestListCollections(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	_ = AddCollection(db, "docs", "/home/user/docs", "**/*.md", nil)

	cols, err := ListCollections(db)
	if err != nil {
		t.Fatalf("ListCollections failed: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 collections, got %d", len(cols))
	}

	names := map[string]bool{}
	for _, c := range cols {
		names[c.Name] = true
	}
	if !names["notes"] || !names["docs"] {
		t.Fatal("missing expected collections")
	}
}

func TestRenameCollection(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddCollection(db, "notes", "/home/user/notes", "**/*.md", nil)
	if err := RenameCollection(db, "notes", "my-notes"); err != nil {
		t.Fatalf("RenameCollection failed: %v", err)
	}

	cols, _ := ListCollections(db)
	for _, c := range cols {
		if c.Name == "notes" {
			t.Fatal("old name should not exist")
		}
		if c.Name == "my-notes" {
			return
		}
	}
	t.Fatal("renamed collection not found")
}

func openMigratedDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openTestDB(t)
	if err := CreateTables(db); err != nil {
		db.Close()
		t.Fatalf("CreateTables failed: %v", err)
	}
	if err := PrepareFTSStatements(db); err != nil {
		db.Close()
		t.Fatalf("prepareFTSStatements failed: %v", err)
	}
	return db
}

func getCollection(t *testing.T, db *sql.DB, name string) (string, string, string) {
	t.Helper()
	var n, p, g string
	err := db.QueryRow("SELECT name, path, glob_pattern FROM collections WHERE name=?", name).Scan(&n, &p, &g)
	if err != nil {
		t.Fatalf("failed to get collection %s: %v", name, err)
	}
	return n, p, g
}
