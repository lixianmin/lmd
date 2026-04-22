package dao

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInit(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "test.sqlite")

	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	defer DB.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Fatal("database file not created")
	}
}

func TestInitCreatesDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "deep", "nested", "db.sqlite")

	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	defer DB.Close()

	if _, err := os.Stat(filepath.Dir(dbPath)); os.IsNotExist(err) {
		t.Fatal("parent directories not created")
	}
}

func TestClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.sqlite")

	if err := Init(dbPath); err != nil {
		t.Fatal(err)
	}
	if err := DB.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if err := DB.Close(); err != nil {
		t.Fatalf("double Close should not error: %v", err)
	}
}
