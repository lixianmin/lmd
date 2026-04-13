package store

import (
	"testing"
)

func TestAddContext(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	err := AddContext(db, "notes", "work", "Work-related notes")
	if err != nil {
		t.Fatalf("AddContext failed: %v", err)
	}
}

func TestAddContextUpdate(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddContext(db, "notes", "work", "v1")
	err := AddContext(db, "notes", "work", "v2")
	if err != nil {
		t.Fatalf("AddContext update failed: %v", err)
	}

	ctx, err := GetContext(db, "notes", "work")
	if err != nil {
		t.Fatalf("GetContext failed: %v", err)
	}
	if ctx != "v2" {
		t.Fatalf("expected v2, got %s", ctx)
	}
}

func TestGetContextNotFound(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_, err := GetContext(db, "notes", "nonexist")
	if err == nil {
		t.Fatal("expected error for nonexistent context")
	}
}

func TestRemoveContext(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddContext(db, "notes", "work", "desc")
	err := RemoveContext(db, "notes", "work")
	if err != nil {
		t.Fatalf("RemoveContext failed: %v", err)
	}

	_, err = GetContext(db, "notes", "work")
	if err == nil {
		t.Fatal("expected error after removal")
	}
}

func TestListContexts(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddContext(db, "notes", "", "global notes context")
	_ = AddContext(db, "notes", "work", "work notes")
	_ = AddContext(db, "docs", "api", "API docs")

	contexts, err := ListContexts(db, "notes")
	if err != nil {
		t.Fatalf("ListContexts failed: %v", err)
	}
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts for notes, got %d", len(contexts))
	}
}

func TestFindBestContext(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	_ = AddContext(db, "notes", "", "collection-level")
	_ = AddContext(db, "notes", "work", "work-level")

	ctx := FindBestContext(db, "notes", "work/project-a.md")
	if ctx != "work-level" {
		t.Fatalf("expected work-level, got %q", ctx)
	}

	ctx2 := FindBestContext(db, "notes", "personal/diary.md")
	if ctx2 != "collection-level" {
		t.Fatalf("expected collection-level, got %q", ctx2)
	}
}

func TestFindBestContextNone(t *testing.T) {
	db := openMigratedDB(t)
	defer db.Close()

	ctx := FindBestContext(db, "notes", "any/path.md")
	if ctx != "" {
		t.Fatalf("expected empty, got %q", ctx)
	}
}
