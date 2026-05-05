package dao

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"time"
)

// MemoryRecord stores the document-level metadata for a memory entry
type MemoryRecord struct {
	Id         int64
	Content    string
	Collection string
	CreatedAt  time.Time
}

const memoryCollectionEpisodic = "@episodic"

// sha256Hash returns SHA-256 hash of data as bytes
func sha256Hash(data []byte) []byte {
	h := sha256.Sum256(data)
	return h[:]
}

// InsertMemory creates a document + single chunk for a memory.
// Follows the same pattern as InsertChunks: inserts into documents, chunks, and chunks_fts
// in a single transaction.
func InsertMemory(content string) (int64, error) {
	hash := hex.EncodeToString(sha256Hash([]byte(content)))
	docid := "mem-" + hash[:12] // 使用 hash 前 12 位（48 bits）作为唯一标识
	path := "/@memory/" + hash[:12]
	title := content
	if len([]rune(title)) > 80 { // 截取前 80 字符用于列表展示
		title = string([]rune(title)[:80])
	}

	var docId int64
	err := withTransaction(func(tx *sql.Tx) error {
		res, err := tx.Exec(
			"INSERT INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time) VALUES (?, ?, ?, ?, ?, ?, 0, 0)",
			docid, memoryCollectionEpisodic, path, title, content, hash,
		)
		if err != nil {
			return err
		}
		docId, _ = res.LastInsertId()

		res, err = tx.Exec(
			"INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, 1, ?, 0, 0, ?)",
			docId, content, hash,
		)
		if err != nil {
			return err
		}
		chunkId, _ := res.LastInsertId()

		// Explicit FTS insert — matched to InsertChunks pattern in chunks_vec.go:73
		_, err = tx.Exec("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)", chunkId, content)
		return err
	})
	return docId, err
}

// GetMemoryByID fetches a memory record by document id
func GetMemoryByID(docId int64) (*MemoryRecord, error) {
	row := withQueryRow(
		"SELECT id, body, collection, created_at FROM documents WHERE id=?",
		docId,
	)
	var rec MemoryRecord
	if err := row.Scan(&rec.Id, &rec.Content, &rec.Collection, &rec.CreatedAt); err != nil {
		return nil, err
	}
	return &rec, nil
}

// DeleteMemory deletes a memory and all associated chunks/vectors/fts entries.
// Uses ON DELETE CASCADE: deleting from documents cascades to chunks,
// but we manually clean chunks_vec and chunks_fts first.
func DeleteMemory(docId int64) error {
	return withTransaction(func(tx *sql.Tx) error {
		chunkRows, err := tx.Query("SELECT id FROM chunks WHERE doc_id=?", docId)
		if err != nil {
			return err
		}

		var chunkIds []int64
		for chunkRows.Next() {
			var cid int64
			if err := chunkRows.Scan(&cid); err != nil {
				chunkRows.Close()
				return err
			}
			chunkIds = append(chunkIds, cid)
		}
		chunkRows.Close()
		if err := chunkRows.Err(); err != nil {
			return err
		}

		for _, cid := range chunkIds {
			if _, err := tx.Exec("DELETE FROM chunks_vec WHERE chunk_id=?", cid); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM chunks_fts WHERE rowid=?", cid); err != nil {
				return err
			}
		}
		tx.Exec("DELETE FROM chunks WHERE doc_id=?", docId)

		res, err := tx.Exec("DELETE FROM documents WHERE id=?", docId)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return sql.ErrNoRows
		}
		return nil
	})
}

// UpdateMemory updates a memory's content, replacing its chunk
func UpdateMemory(docId int64, content string) error {
	hash := hex.EncodeToString(sha256Hash([]byte(content)))
	docid := "mem-" + hash[:12] // 使用 hash 前 12 位（48 bits）作为唯一标识
	title := content
	if len([]rune(title)) > 80 { // 截取前 80 字符用于列表展示
		title = string([]rune(title)[:80])
	}

	return withTransaction(func(tx *sql.Tx) error {
		// Delete old chunks
		chunkRows, err := tx.Query("SELECT id FROM chunks WHERE doc_id=?", docId)
		if err != nil {
			return err
		}
		var chunkIds []int64
		for chunkRows.Next() {
			var cid int64
			if err := chunkRows.Scan(&cid); err != nil {
				chunkRows.Close()
				return err
			}
			chunkIds = append(chunkIds, cid)
		}
		chunkRows.Close()
		if err := chunkRows.Err(); err != nil {
			return err
		}

		for _, cid := range chunkIds {
			if _, err := tx.Exec("DELETE FROM chunks_vec WHERE chunk_id=?", cid); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM chunks_fts WHERE rowid=?", cid); err != nil {
				return err
			}
		}
		tx.Exec("DELETE FROM chunks WHERE doc_id=?", docId)

		// Insert new chunk
		res, err := tx.Exec(
			"INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, 1, ?, 0, 0, ?)",
			docId, content, hash,
		)
		if err != nil {
			return err
		}
		chunkId, _ := res.LastInsertId()

		_, err = tx.Exec("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)", chunkId, content)
		if err != nil {
			return err
		}

		// Update document
		_, err = tx.Exec(
			"UPDATE documents SET docid=?, body=?, hash=?, title=?, updated_at=DATETIME('now', '+8 hours') WHERE id=?",
			docid, content, hash, title, docId,
		)
		return err
	})
}

// ListMemories returns memories ordered by creation time descending
func ListMemories(limit int) ([]MemoryRecord, error) {
	query := "SELECT id, body, collection, created_at FROM documents WHERE collection = ? ORDER BY created_at DESC"
	args := []any{memoryCollectionEpisodic}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var rec MemoryRecord
		if err := rows.Scan(&rec.Id, &rec.Content, &rec.Collection, &rec.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, rec)
	}
	return results, rows.Err()
}

// CountMemories returns the total number of memory documents
func CountMemories() (int, error) {
	var count int
	err := withQueryRow(
		"SELECT COUNT(*) FROM documents WHERE collection = ?",
		memoryCollectionEpisodic,
	).Scan(&count)
	return count, err
}
