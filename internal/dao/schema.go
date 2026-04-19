package dao

func createTables() error {
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
			embedding float[1024] distance_metric=cosine
		)`,
		`CREATE INDEX IF NOT EXISTS idx_documents_collection_path ON documents(collection, path)`,
		`CREATE INDEX IF NOT EXISTS idx_chunks_doc_id ON chunks(doc_id)`,
	}
	for _, s := range stmts {
		if _, err := DB.db.Exec(s); err != nil {
			return err
		}
	}

	_, _ = DB.db.Exec("ALTER TABLE documents ADD COLUMN file_mod_time INTEGER DEFAULT 0")
	return nil
}
