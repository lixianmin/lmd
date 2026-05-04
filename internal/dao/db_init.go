package dao

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/lixianmin/logo"
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
	DB.db, err = sql.Open("sqlite3", dbPath+"?_journal_mode=wal&_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return err
	}

	if err := createTables(); err != nil {
		return err
	}
	if err := migrateMemories(); err != nil {
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

func migrateMemories() error {
	var tableCount int
	err := DB.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='memories'").Scan(&tableCount)
	if err != nil {
		return err
	}
	if tableCount == 0 {
		return nil
	}

	rows, err := withQuery("SELECT id, content, type FROM memories")
	if err != nil {
		return err
	}
	defer rows.Close()

	type memRow struct {
		content string
		mType   string
	}
	var mems []memRow
	for rows.Next() {
		var m memRow
		if err := rows.Scan(new(int64), &m.content, &m.mType); err != nil {
			return err
		}
		mems = append(mems, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	if len(mems) > 0 {
		err = withTransaction(func(tx *sql.Tx) error {
			docStmt, err := tx.Prepare(`INSERT OR IGNORE INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time) VALUES (?, ?, ?, ?, ?, ?, 0, 0)`)
			if err != nil {
				return err
			}
			defer docStmt.Close()

			chunkStmt, err := tx.Prepare("INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, 0, ?, 0, 0, ?)")
			if err != nil {
				return err
			}
			defer chunkStmt.Close()

			for _, m := range mems {
				contentHash := sha256.Sum256([]byte(m.content))
				hashStr := hex.EncodeToString(contentHash[:])
				// 使用 hash 前 12 位（48 bits）作为唯一标识，冲突概率极低
				shortHash := hashStr[:12]

				collection := "@knowledge"
				if m.mType == "episode" {
					collection = "@episodic"
				}

				docid := "mem-" + shortHash
				path := "/@memory/" + shortHash
			title := m.content
			// 标题截取前 80 个字符，用于列表展示
			runes := []rune(title)
			if len(runes) > 80 {
				title = string(runes[:80])
			}

				res, err := docStmt.Exec(docid, collection, path, title, m.content, hashStr)
				if err != nil {
					return err
				}
				docId, _ := res.LastInsertId()
				if docId == 0 {
					continue
				}

			chunkRes, err := chunkStmt.Exec(docId, m.content, hashStr)
			if err != nil {
				return err
			}
			chunkId, _ := chunkRes.LastInsertId()
			if _, err := tx.Exec("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)", chunkId, m.content); err != nil {
				return err
			}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}

	for _, tbl := range []string{"memories_vec", "memories_fts", "memories"} {
		if _, err := DB.db.Exec("DROP TABLE IF EXISTS " + tbl); err != nil {
			logo.Warn("drop %s: %v", tbl, err)
		}
	}
	logo.Info("migrated %d memories to documents+chunks", len(mems))
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
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_documents_collection_path ON documents(collection, path)`,
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
