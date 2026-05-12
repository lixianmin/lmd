package dao

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type DocumentRecord struct {
	Id          int64
	DocId       string
	Collection  string
	Path        string
	Title       string
	Body        string
	Hash        string
	FileSize    int64
	FileModTime int64
	SourceDocId int64
	ModifiedAt  time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func generateDocId(collection, path, hash string) string {
	raw := fmt.Sprintf("%s:%s:%s", collection, path, hash)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:])
}

func ShortDocId(docId string) string {
	if len(docId) > 8 {
		return docId[:8]
	}
	return docId
}

func InsertDocument(collection, path, title, body string, fileSize int64, hash string) (int64, error) {
	docId := generateDocId(collection, path, hash)
	result, err := WithExec(
		"INSERT INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time, modified_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, 0, DATETIME('now','+8 hours'), DATETIME('now','+8 hours'), DATETIME('now','+8 hours'))",
		docId, collection, path, title, body, hash, fileSize,
	)
	if err != nil {
		return 0, err
	}
	id, _ := result.LastInsertId()
	return id, nil
}

func CompleteDocument(docId int64, fileModTime int64) error {
	_, err := WithExec(
		"UPDATE documents SET file_mod_time=?, updated_at=DATETIME('now','+8 hours') WHERE id=?",
		fileModTime, docId,
	)
	return err
}

func UpsertDocument(doc *DocumentRecord) error {
	doc.DocId = generateDocId(doc.Collection, doc.Path, doc.Hash)

	res, err := WithExec(`
		INSERT INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, modified_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, DATETIME('now', '+8 hours'))
		ON CONFLICT(collection, path) DO UPDATE SET
			docid=excluded.docid, title=excluded.title, body=excluded.body,
			hash=excluded.hash, file_size=excluded.file_size, file_mod_time=excluded.file_mod_time,
			source_doc_id=excluded.source_doc_id,
			modified_at=DATETIME('now', '+8 hours'), updated_at=DATETIME('now', '+8 hours')
	`, doc.DocId, doc.Collection, doc.Path, doc.Title, doc.Body, doc.Hash, doc.FileSize, doc.FileModTime, doc.SourceDocId)
	if err != nil {
		return err
	}

	doc.Id, _ = res.LastInsertId()
	if doc.Id == 0 {
		err := withQueryRow("SELECT id FROM documents WHERE collection=? AND path=?",
			doc.Collection, doc.Path).Scan(&doc.Id)
		if err != nil {
			return err
		}
	}
	return nil
}

func GetDocumentByDocId(docId string) (*DocumentRecord, error) {
	rows, err := withQuery("SELECT id, docid, collection, path, title, body, hash, file_size, source_doc_id, created_at, updated_at FROM documents WHERE docid LIKE ?", docId+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
			&doc.Hash, &doc.FileSize, &doc.SourceDocId, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(docs) == 0 {
		return nil, errors.New("document not found")
	}
	if len(docs) > 1 {
		return nil, fmt.Errorf("ambiguous docid '%s' matches %d documents, use a longer prefix", docId, len(docs))
	}
	return &docs[0], nil
}

func GetDocumentByPath(collection, path string) (*DocumentRecord, error) {
	return getDocument("WHERE collection=? AND path=?", collection, path)
}

func GetDocumentById(id int64) (*DocumentRecord, error) {
	return getDocument("WHERE id=?", id)
}

func GetDocumentBySourceDocId(collection string, sourceDocId int64) (*DocumentRecord, error) {
	return getDocument("WHERE collection=? AND source_doc_id=?", collection, sourceDocId)
}

func getDocument(whereClause string, args ...any) (*DocumentRecord, error) {
	query := "SELECT id, docid, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, created_at, updated_at FROM documents " + whereClause
	var doc DocumentRecord
	err := withQueryRow(query, args...).Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
		&doc.Hash, &doc.FileSize, &doc.FileModTime, &doc.SourceDocId, &doc.CreatedAt, &doc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("document not found")
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func deleteDocChunksAndVecs(tx *sql.Tx, docId int64) error {
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
	return nil
}

func DeleteDocument(id int64) error {
	return withTransaction(func(tx *sql.Tx) error {
		if err := deleteDocChunksAndVecs(tx, id); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM documents WHERE id=?", id)
		return err
	})
}

func DeleteDocumentAndSummary(docId int64) error {
	return withTransaction(func(tx *sql.Tx) error {
		rows, err := tx.Query("SELECT id FROM documents WHERE source_doc_id=?", docId)
		if err != nil {
			return err
		}
		var summaryIds []int64
		for rows.Next() {
			var sid int64
			if err := rows.Scan(&sid); err != nil {
				rows.Close()
				return err
			}
			summaryIds = append(summaryIds, sid)
		}
		rows.Close()

		for _, sid := range summaryIds {
			if err := deleteDocChunksAndVecs(tx, sid); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM documents WHERE id=?", sid); err != nil {
				return err
			}
		}

		if err := deleteDocChunksAndVecs(tx, docId); err != nil {
			return err
		}
		_, err = tx.Exec("DELETE FROM documents WHERE id=?", docId)
		return err
	})
}

func GetDocumentsByIds(ids []int64) ([]DocumentRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("SELECT id, docid, collection, path, title, body, hash, file_size, source_doc_id, created_at, updated_at FROM documents WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
			&doc.Hash, &doc.FileSize, &doc.SourceDocId, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		results = append(results, doc)
	}
	return results, rows.Err()
}

func ListDocumentsByCollection(collection string) ([]DocumentRecord, error) {
	rows, err := withQuery("SELECT id, docid, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, created_at, updated_at FROM documents WHERE collection=?", collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title,
			&doc.Body, &doc.Hash, &doc.FileSize, &doc.FileModTime, &doc.SourceDocId, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func CountDocuments() (int, error) {
	if DB == nil || DB.db == nil {
		return 0, nil
	}
	var count int
	err := DB.db.QueryRow("SELECT COUNT(*) FROM documents").Scan(&count)
	return count, err
}

func GetDocumentHash(collection, path string) (string, error) {
	var hash string
	err := withQueryRow("SELECT hash FROM documents WHERE collection=? AND path=?", collection, path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", errors.New("document not found")
	}
	return hash, err
}

func UpsertHydeData(sourceDocId int64, hash, content, tokenizedContent string, vec []float32) (int64, error) {
	var docId int64
	err := withTransaction(func(tx *sql.Tx) error {
		existingRows, err := tx.Query("SELECT id FROM documents WHERE collection='@hyde' AND source_doc_id=?", sourceDocId)
		if err != nil {
			return err
		}
		var existingIds []int64
		for existingRows.Next() {
			var eid int64
			if err := existingRows.Scan(&eid); err != nil {
				existingRows.Close()
				return err
			}
			existingIds = append(existingIds, eid)
		}
		existingRows.Close()

		for _, did := range existingIds {
			if _, err := tx.Exec("DELETE FROM chunks_fts WHERE rowid IN (SELECT id FROM chunks WHERE doc_id=?)", did); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM chunks_vec WHERE chunk_id IN (SELECT id FROM chunks WHERE doc_id=?)", did); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM chunks WHERE doc_id=?", did); err != nil {
				return err
			}
			if _, err := tx.Exec("DELETE FROM documents WHERE id=?", did); err != nil {
				return err
			}
		}

		docIdStr := generateDocId("@hyde", fmt.Sprintf("%d", sourceDocId), hash)
		res, err := tx.Exec(`INSERT INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, modified_at)
			VALUES (?, '@hyde', ?, '', '', ?, 0, 0, ?, DATETIME('now', '+8 hours'))`,
			docIdStr, fmt.Sprintf("/@hyde/%d", sourceDocId), hash, sourceDocId)
		if err != nil {
			return err
		}
		docId, _ = res.LastInsertId()

		chunkRes, err := tx.Exec("INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, 0, ?, 0, 0, ?)", docId, content, hash)
		if err != nil {
			return err
		}
		chunkId, _ := chunkRes.LastInsertId()

		_, err = tx.Exec("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)", chunkId, tokenizedContent)
		if err != nil {
			return err
		}

		serialized, err := sqlite_vec.SerializeFloat32(padVector(vec))
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT INTO chunks_vec(chunk_id, embedding, doc_id, collection) VALUES (?, ?, ?, '@hyde')", chunkId, serialized, docId)
		return err
	})
	return docId, err
}

func FindDocsWithMissingEmbeddings(limit int) ([]DocumentRecord, error) {
	query := `
		SELECT d.id, d.docid, d.collection, d.path, d.title, d.body, d.hash, d.file_size, d.file_mod_time, d.source_doc_id, d.created_at, d.updated_at
		FROM documents d
		WHERE d.collection NOT LIKE '@%'
		AND EXISTS (SELECT 1 FROM chunks c WHERE c.doc_id = d.id)
		AND NOT EXISTS (SELECT 1 FROM chunks_vec_rowids v JOIN chunks c ON c.id = v.chunk_id WHERE c.doc_id = d.id)
		LIMIT ?`
	rows, err := withQuery(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
			&doc.Hash, &doc.FileSize, &doc.FileModTime, &doc.SourceDocId, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}
