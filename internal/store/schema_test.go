package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestMigrateCreatesTables(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	tables := []string{"collections", "path_contexts", "documents", "chunks", "embed_status", "_meta"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %s not found: %v", table, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("first Migrate failed: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second Migrate failed: %v", err)
	}
}

func TestMigrateSetsVersion(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	var version string
	err := db.QueryRow("SELECT value FROM _meta WHERE key='schema_version'").Scan(&version)
	if err != nil {
		t.Fatalf("failed to query schema_version: %v", err)
	}
	if version != "1" {
		t.Fatalf("expected schema_version=1, got %s", version)
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
