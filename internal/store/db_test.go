package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenDBCreatesFile(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created")
	}
}

func TestOpenDBEnablesWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	var journalMode string
	err = db.QueryRow("PRAGMA journal_mode").Scan(&journalMode)
	if err != nil {
		t.Fatalf("failed to query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %s", journalMode)
	}
}

func TestOpenDBCreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "subdir", "nested", "test.sqlite")

	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatalf("OpenDB failed: %v", err)
	}
	defer db.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file was not created in nested directory")
	}
}
