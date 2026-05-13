package dao

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
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

func InsertDocument(collection, path, title, body string, fileSize int64, fileModTime int64, hash string) (int64, error) {
	var id int64
	err := withTransaction(func(tx *sql.Tx) error {
		docId := generateDocId(collection, path, hash)
		result, err := tx.Exec(
			"INSERT INTO documents (doc_id, collection, path, title, body, hash, file_size, file_mod_time, modified_at, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, DATETIME('now','+8 hours'), DATETIME('now','+8 hours'), DATETIME('now','+8 hours'))",
			docId, collection, path, title, body, hash, fileSize, fileModTime,
		)
		if err != nil {
			return err
		}
		id, _ = result.LastInsertId()

		return insertDocumentsLog(tx, id, "INSERT", map[string]interface{}{
			"doc_id":      docId,
			"collection": collection,
			"path":       path,
		})
	})
	return id, err
}

func UpdateFileModTime(docId int64, fileModTime int64) error {
	return withTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec(
			"UPDATE documents SET file_mod_time=?, updated_at=DATETIME('now','+8 hours') WHERE id=?",
			fileModTime, docId,
		)
		if err != nil {
			return err
		}

		return insertDocumentsLog(tx, docId, "UPDATE", map[string]interface{}{
			"file_mod_time": fileModTime,
		})
	})
}

func UpsertDocument(doc *DocumentRecord) error {
	return withTransaction(func(tx *sql.Tx) error {
		doc.DocId = generateDocId(doc.Collection, doc.Path, doc.Hash)

		nodeId := doc.DocId
		collection := doc.Collection
		path := doc.Path
		title := doc.Title
		body := doc.Body
		hash := doc.Hash
		fileSize := doc.FileSize
		fileModTime := doc.FileModTime
		sourceDocId := doc.SourceDocId

		var oldId int64
		tx.QueryRow("SELECT id FROM documents WHERE collection=? AND path=?", collection, path).Scan(&oldId)

		res, err := tx.Exec(`
			INSERT INTO documents (doc_id, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, modified_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, DATETIME('now', '+8 hours'))
			ON CONFLICT(collection, path) DO UPDATE SET
				doc_id=excluded.doc_id, title=excluded.title, body=excluded.body,
				hash=excluded.hash, file_size=excluded.file_size, file_mod_time=excluded.file_mod_time,
				source_doc_id=excluded.source_doc_id,
				modified_at=DATETIME('now', '+8 hours'), updated_at=DATETIME('now', '+8 hours')
		`, nodeId, collection, path, title, body, hash, fileSize, fileModTime, sourceDocId)
		if err != nil {
			return err
		}

		if oldId != 0 {
			doc.Id = oldId
		} else {
			doc.Id, _ = res.LastInsertId()
		}

		if oldId == 0 {
			return insertDocumentsLog(tx, doc.Id, "INSERT", map[string]interface{}{
				"doc_id":      nodeId,
				"collection": collection,
				"path":       path,
			})
		}
		return insertDocumentsLog(tx, doc.Id, "UPDATE", map[string]interface{}{
			"doc_id":      nodeId,
			"collection": collection,
			"path":       path,
		})
	})
}

func GetDocumentByDocId(docId string) (*DocumentRecord, error) {
	rows, err := withQuery("SELECT id, doc_id, collection, path, title, body, hash, file_size, source_doc_id, created_at, updated_at FROM documents WHERE doc_id LIKE ?", docId+"%")
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
		return nil, fmt.Errorf("ambiguous doc_id '%s' matches %d documents, use a longer prefix", docId, len(docs))
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
	query := "SELECT id, doc_id, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, created_at, updated_at FROM documents " + whereClause
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
		if err := insertChunksLog(tx, cid, docId, "DELETE", map[string]interface{}{
			"doc_id": docId, "reason": "document_deleted",
		}); err != nil {
			return err
		}
	}
	return nil
}

func DeleteDocument(id int64) error {
	return withTransaction(func(tx *sql.Tx) error {
		var docDocId, docPath string
		tx.QueryRow("SELECT doc_id, path FROM documents WHERE id=?", id).Scan(&docDocId, &docPath)

		if err := deleteDocChunksAndVecs(tx, id); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM documents WHERE id=?", id); err != nil {
			return err
		}
		return insertDocumentsLog(tx, id, "DELETE", map[string]interface{}{
			"doc_id": docDocId, "path": docPath,
		})
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
	query := fmt.Sprintf("SELECT id, doc_id, collection, path, title, body, hash, file_size, source_doc_id, created_at, updated_at FROM documents WHERE id IN (%s)", strings.Join(placeholders, ","))
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
	rows, err := withQuery("SELECT id, doc_id, collection, path, title, body, hash, file_size, file_mod_time, source_doc_id, created_at, updated_at FROM documents WHERE collection=?", collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
			&doc.Hash, &doc.FileSize, &doc.FileModTime, &doc.SourceDocId, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		result = append(result, doc)
	}
	return result, rows.Err()
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

func FindDocsWithMissingEmbeddings(limit int) ([]DocumentRecord, error) {
	query := `
		SELECT d.id, d.doc_id, d.collection, d.path, d.title, d.body, d.hash, d.file_size, d.file_mod_time, d.source_doc_id, d.created_at, d.updated_at
		FROM documents d
		WHERE d.collection NOT LIKE '@%'
		AND EXISTS (SELECT 1 FROM chunks c WHERE c.doc_id = d.id)
		AND NOT EXISTS (SELECT 1 FROM chunks_vec v WHERE v.chunk_id IN (SELECT c.id FROM chunks c WHERE c.doc_id = d.id))
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
