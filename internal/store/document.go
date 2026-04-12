package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

type DocumentRecord struct {
	ID         int64
	DocID      string
	Collection string
	Path       string
	Title      string
	Body       string
	Hash       string
	FileSize   int64
	ModifiedAt time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func generateDocID(collection, path, hash string) string {
	raw := fmt.Sprintf("%s:%s:%s", collection, path, hash)
	h := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(h[:3])
}

func UpsertDocument(db *sql.DB, doc *DocumentRecord, tokenizedBody, tokenizedTitle string) error {
	doc.DocID = generateDocID(doc.Collection, doc.Path, doc.Hash)

	var existingID int64
	err := db.QueryRow(
		"SELECT id FROM documents WHERE collection=? AND path=?",
		doc.Collection, doc.Path,
	).Scan(&existingID)

	if err == sql.ErrNoRows {
		res, err := db.Exec(
			`INSERT INTO documents (docid, collection, path, title, body, hash, file_size, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, DATETIME('now', '+8 hours'))`,
			doc.DocID, doc.Collection, doc.Path, doc.Title, doc.Body, doc.Hash, doc.FileSize,
		)
		if err != nil {
			return err
		}
		doc.ID, _ = res.LastInsertId()

		_, err = db.Exec(
			"INSERT INTO documents_fts (rowid, tokens, title_tokens) VALUES (?, ?, ?)",
			doc.ID, tokenizedBody, tokenizedTitle,
		)
		return err
	}

	if err != nil {
		return err
	}

	doc.ID = existingID
	_, err = db.Exec(
		`UPDATE documents SET docid=?, title=?, body=?, hash=?, file_size=?, modified_at=DATETIME('now', '+8 hours'), updated_at=DATETIME('now', '+8 hours')
		 WHERE id=?`,
		doc.DocID, doc.Title, doc.Body, doc.Hash, doc.FileSize, existingID,
	)
	if err != nil {
		return err
	}

	_, err = db.Exec(
		"UPDATE documents_fts SET tokens=?, title_tokens=? WHERE rowid=?",
		tokenizedBody, tokenizedTitle, existingID,
	)
	return err
}

func GetDocumentByDocID(db *sql.DB, docID string) (*DocumentRecord, error) {
	return getDocument(db, "WHERE docid=?", docID)
}

func GetDocumentByPath(db *sql.DB, collection, path string) (*DocumentRecord, error) {
	return getDocument(db, "WHERE collection=? AND path=?", collection, path)
}

func getDocument(db *sql.DB, whereClause string, args ...any) (*DocumentRecord, error) {
	query := "SELECT id, docid, collection, path, title, body, hash, file_size, created_at, updated_at FROM documents " + whereClause
	row := db.QueryRow(query, args...)

	var doc DocumentRecord
	err := row.Scan(&doc.ID, &doc.DocID, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
		&doc.Hash, &doc.FileSize, &doc.CreatedAt, &doc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("document not found")
	}
	if err != nil {
		return nil, err
	}
	return &doc, nil
}

func DeleteDocument(db *sql.DB, id int64) error {
	_, err := db.Exec("DELETE FROM documents WHERE id=?", id)
	return err
}

func ListDocumentsByCollection(db *sql.DB, collection string) ([]DocumentRecord, error) {
	rows, err := db.Query(
		"SELECT id, docid, collection, path, title, body, hash, file_size, created_at, updated_at FROM documents WHERE collection=?",
		collection,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.ID, &doc.DocID, &doc.Collection, &doc.Path, &doc.Title,
			&doc.Body, &doc.Hash, &doc.FileSize, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}

func GetDocumentHash(db *sql.DB, collection, path string) (string, error) {
	var hash string
	err := db.QueryRow("SELECT hash FROM documents WHERE collection=? AND path=?", collection, path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", errors.New("document not found")
	}
	return hash, err
}
