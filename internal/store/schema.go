package store

import (
	"database/sql"
)

type Migration struct {
	Version int
	Up      func(tx *sql.Tx) error
}

var migrations = []Migration{
	{Version: 1, Up: migrateV1},
}

func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS _meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)
	`); err != nil {
		return err
	}

	var current int
	row := db.QueryRow("SELECT CAST(value AS INTEGER) FROM _meta WHERE key='schema_version'")
	if err := row.Scan(&current); err != nil {
		if err == sql.ErrNoRows {
			current = 0
		} else {
			return err
		}
	}

	for _, m := range migrations {
		if m.Version > current {
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			if err := m.Up(tx); err != nil {
				tx.Rollback()
				return err
			}
			if _, err := tx.Exec(
				"INSERT OR REPLACE INTO _meta (key, value) VALUES ('schema_version', ?)", m.Version,
			); err != nil {
				tx.Rollback()
				return err
			}
			if err := tx.Commit(); err != nil {
				return err
			}
		}
	}
	return nil
}

func migrateV1(tx *sql.Tx) error {
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
		`CREATE VIRTUAL TABLE IF NOT EXISTS documents_fts USING fts5(
			tokens,
			title_tokens,
			content='documents',
			content_rowid='id',
			tokenize='unicode61'
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
		`CREATE TABLE IF NOT EXISTS embed_status (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			chunk_id    INTEGER NOT NULL REFERENCES chunks(id) ON DELETE CASCADE,
			model_name  TEXT NOT NULL,
			embedded_at DATETIME DEFAULT (DATETIME('now', '+8 hours')),
			UNIQUE(chunk_id, model_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_collection ON documents(collection)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_hash ON documents(hash)`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	return nil
}
