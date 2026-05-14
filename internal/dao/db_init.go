package dao

import (
	"database/sql"
	"fmt"
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
	DB.db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=wal&_foreign_keys=on&_busy_timeout=10000")
	if err != nil {
		return err
	}
	DB.db.SetMaxOpenConns(4)
	DB.db.SetMaxIdleConns(1)

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
	if DB == nil || DB.db == nil {
		return fmt.Errorf("database not initialized or already closed")
	}
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

func WithQuery(query string, args ...any) (*sql.Rows, error) {
	return DB.db.Query(query, args...)
}

func withQuery(query string, args ...any) (*sql.Rows, error) {
	return DB.db.Query(query, args...)
}

func withQueryRow(query string, args ...any) *sql.Row {
	return DB.db.QueryRow(query, args...)
}

func createTables() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS collections (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL UNIQUE,
			path            TEXT NOT NULL,
			glob_pattern    TEXT DEFAULT '**/*.{md,txt}',
			ignore_patterns TEXT,
			created_at      DATETIME DEFAULT (DATETIME('now', '+8 hours')),
			updated_at      DATETIME DEFAULT (DATETIME('now', '+8 hours'))
		)`,
		`CREATE TABLE IF NOT EXISTS documents (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id      TEXT NOT NULL UNIQUE,
			collection  TEXT NOT NULL,
			path        TEXT NOT NULL,
			title       TEXT,
			body        TEXT NOT NULL,
			hash        TEXT NOT NULL,
			file_size   INTEGER,
			source_doc_id INTEGER DEFAULT 0,
			modified_at DATETIME,
			created_at  DATETIME DEFAULT (DATETIME('now', '+8 hours')),
			updated_at  DATETIME DEFAULT (DATETIME('now', '+8 hours')),
			file_mod_time INTEGER DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS chunks (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id      INTEGER NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			seq         INTEGER NOT NULL,
			content     TEXT NOT NULL,
			position    INTEGER NOT NULL,
			token_count INTEGER,
			hash        TEXT NOT NULL,
			UNIQUE(doc_id, seq)
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
			content,
			content = 'chunks',
			content_rowid = 'id',
			tokenize = 'porter unicode61'
		)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_vec USING vec0(
			chunk_id INTEGER PRIMARY KEY,
			embedding float[1024] distance_metric=cosine,
			doc_id INTEGER,
			collection TEXT
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_collection_path ON documents(collection, path)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_source_doc_id ON documents(source_doc_id)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_doc_id ON chunks(doc_id)`,
		`CREATE TABLE IF NOT EXISTS documents_log (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id      INTEGER NOT NULL,
			operation   TEXT NOT NULL,
			data_json   TEXT NOT NULL,
			created_at  DATETIME DEFAULT (DATETIME('now', '+8 hours'))
		)`,
		`CREATE TABLE IF NOT EXISTS chunks_log (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			chunk_id    INTEGER NOT NULL,
			doc_id      INTEGER NOT NULL,
			operation   TEXT NOT NULL,
			data_json   TEXT NOT NULL,
			created_at  DATETIME DEFAULT (DATETIME('now', '+8 hours'))
		)`,
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
	}

	for _, s := range stmts {
		if _, err := DB.db.Exec(s); err != nil {
			return err
		}
	}

	return nil
}
