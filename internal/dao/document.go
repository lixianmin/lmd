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
	query := "SELECT id, docid, collection, path, title, body, hash, file_size, source_doc_id, created_at, updated_at FROM documents " + whereClause
	var doc DocumentRecord
	err := withQueryRow(query, args...).Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
		&doc.Hash, &doc.FileSize, &doc.SourceDocId, &doc.CreatedAt, &doc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("document not found")
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func DeleteDocument(id int64) error {
	return withTransaction(func(tx *sql.Tx) error {
		chunkRows, err := tx.Query("SELECT id FROM chunks WHERE doc_id=?", id)
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
		_, err = tx.Exec("DELETE FROM documents WHERE id=?", id)
		return err
	})
}

func TouchDocument(id int64) error {
	_, err := WithExec("UPDATE documents SET updated_at=DATETIME('now', '+8 hours') WHERE id=?", id)
	return err
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
