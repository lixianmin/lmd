package store

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

func OpenDB(dbPath string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=wal&_foreign_keys=on")
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func OpenAndMigrate(dbPath string) (*sql.DB, error) {
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
