package dao

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
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

	var existingID int64
	err := DB.db.QueryRow(
		"SELECT id FROM documents WHERE collection=? AND path=?",
		doc.Collection, doc.Path,
	).Scan(&existingID)

	if err == sql.ErrNoRows {
		res, err := WithExec(
			`INSERT INTO documents (docid, collection, path, title, body, hash, file_size, file_mod_time, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, DATETIME('now', '+8 hours'))`,
			doc.DocId, doc.Collection, doc.Path, doc.Title, doc.Body, doc.Hash, doc.FileSize, doc.FileModTime,
		)
		if err != nil {
			return err
		}
		doc.Id, _ = res.LastInsertId()
		return nil
	}

	if err != nil {
		return err
	}

	doc.Id = existingID

	_, err = WithExec(
		`UPDATE documents SET docid=?, title=?, body=?, hash=?, file_size=?, file_mod_time=?, modified_at=DATETIME('now', '+8 hours'), updated_at=DATETIME('now', '+8 hours') WHERE id=?`,
		doc.DocId, doc.Title, doc.Body, doc.Hash, doc.FileSize, doc.FileModTime, existingID,
	)
	return err
}

func GetDocumentByDocId(docId string) (*DocumentRecord, error) {
	var doc DocumentRecord

	err := withQueryRow("SELECT id, docid, collection, path, title, body, hash, file_size, created_at, updated_at FROM documents WHERE docid LIKE ?", docId+"%").Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
		&doc.Hash, &doc.FileSize, &doc.CreatedAt, &doc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("document not found")
	}
	if err != nil {
		return nil, err
	}

	var count int
	if err := withQueryRow("SELECT COUNT(*) FROM documents WHERE docid LIKE ?", docId+"%").Scan(&count); err != nil {
		return nil, err
	}
	if count > 1 {
		return nil, fmt.Errorf("ambiguous docid '%s' matches %d documents, use a longer prefix", docId, count)
	}

	return &doc, nil
}

func GetDocumentByPath(collection, path string) (*DocumentRecord, error) {
	return getDocument("WHERE collection=? AND path=?", collection, path)
}

func GetDocumentById(id int64) (*DocumentRecord, error) {
	return getDocument("WHERE id=?", id)
}

func getDocument(whereClause string, args ...any) (*DocumentRecord, error) {
	query := "SELECT id, docid, collection, path, title, body, hash, file_size, created_at, updated_at FROM documents " + whereClause
	var doc DocumentRecord
	err := withQueryRow(query, args...).Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
		&doc.Hash, &doc.FileSize, &doc.CreatedAt, &doc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("document not found")
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func DeleteDocument(id int64) error {
	_, err := WithExec("DELETE FROM documents WHERE id=?", id)
	return err
}

func ListDocumentsByCollection(collection string) ([]DocumentRecord, error) {
	rows, err := withQuery("SELECT id, docid, collection, path, title, body, hash, file_size, file_mod_time, created_at, updated_at FROM documents WHERE collection=?", collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.Id, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title,
			&doc.Body, &doc.Hash, &doc.FileSize, &doc.FileModTime, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func CountDocuments() (int, error) {
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
