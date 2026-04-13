package store

import (
	"database/sql"
	"os"
	"path/filepath"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	sqlite_vec.Auto()
}

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

func OpenAndInit(dbPath string) (*sql.DB, error) {
	db, err := OpenDB(dbPath)
	if err != nil {
		return nil, err
	}
	if err := CreateTables(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := PrepareFTSStatements(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
