package dao

import (
	"database/sql"
	"os"
	"path/filepath"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

var DB *Store

type Store struct {
	db *sql.DB
}

func init() {
	sqlite_vec.Auto()
}

func Init(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return err
	}

	var err error
	DB = &Store{}
	DB.db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=wal&_foreign_keys=on")
	if err != nil {
		return err
	}

	if _, err := DB.db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		DB.db.Close()
		return err
	}

	if err := createTables(); err != nil {
		return err
	}
	return prepareFTSStatements()
}

func (my *Store) Close() error {
	if my.db != nil {
		return my.db.Close()
	}
	return nil
}

func withTransaction(fn func(tx *sql.Tx) error) error {
	tx, err := DB.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func WithExec(query string, args ...any) (sql.Result, error) {
	stmt, err := DB.db.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	return stmt.Exec(args...)
}

func withQuery(query string, args ...any) (*sql.Rows, error) {
	stmt, err := DB.db.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()
	return stmt.Query(args...)
}

func withQueryRow(query string, args ...any) *sql.Row {
	stmt, err := DB.db.Prepare(query)
	if err != nil {
		return DB.db.QueryRow(query, args...)
	}
	defer stmt.Close()
	return stmt.QueryRow(args...)
}
