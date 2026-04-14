package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestCreateTablesCreatesAllTables(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := CreateTables(db); err != nil {
		t.Fatalf("CreateTables failed: %v", err)
	}

	tables := []string{"collections", "path_contexts", "documents", "chunks"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}

	var vecName string
	err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='chunks_vec'").Scan(&vecName)
	if err != nil {
		t.Errorf("virtual table chunks_vec not found: %v", err)
	}

	var ftsName string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='chunks_fts'").Scan(&ftsName)
	if err != nil {
		t.Errorf("virtual table chunks_fts not found: %v", err)
	}
}

func TestCreateTablesIdempotent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := CreateTables(db); err != nil {
		t.Fatalf("first CreateTables failed: %v", err)
	}
	if err := CreateTables(db); err != nil {
		t.Fatalf("second CreateTables failed: %v", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	return db
}
