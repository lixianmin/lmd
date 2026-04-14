package store

import "database/sql"

func CreateTables(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS collections (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			name            TEXT NOT NULL UNIQUE,
			path            TEXT NOT NULL,
			glob_pattern    TEXT DEFAULT '**/*.md',
			ignore_patterns TEXT,
			created_at      DATETIME DEFAULT (DATETIME('now', '+8 hours')),
			updated_at      DATETIME DEFAULT (DATETIME('now', '+8 hours'))
		)`,
		`CREATE TABLE IF NOT EXISTS path_contexts (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			collection  TEXT NOT NULL,
			path        TEXT NOT NULL DEFAULT '',
			context     TEXT NOT NULL,
			UNIQUE(collection, path)
		)`,
		`CREATE TABLE IF NOT EXISTS documents (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			docid       TEXT NOT NULL UNIQUE,
			collection  TEXT NOT NULL,
			path        TEXT NOT NULL,
			title       TEXT,
			body        TEXT NOT NULL,
			hash        TEXT NOT NULL,
			file_size   INTEGER,
			modified_at DATETIME,
			created_at  DATETIME DEFAULT (DATETIME('now', '+8 hours')),
			updated_at  DATETIME DEFAULT (DATETIME('now', '+8 hours'))
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
			embedding float[1024]
		)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_collection ON documents(collection)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_hash ON documents(hash)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	return nil
}
