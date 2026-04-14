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
	ID         int64
	DocId      string
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
		stmt, err := DB.db.Prepare(
			`INSERT INTO documents (docid, collection, path, title, body, hash, file_size, modified_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, DATETIME('now', '+8 hours'))`,
		)
		if err != nil {
			return err
		}
		defer stmt.Close()

		res, err := stmt.Exec(doc.DocId, doc.Collection, doc.Path, doc.Title, doc.Body, doc.Hash, doc.FileSize)
		if err != nil {
			return err
		}
		doc.ID, _ = res.LastInsertId()
		return nil
	}

	if err != nil {
		return err
	}

	doc.ID = existingID

	updateStmt, err := DB.db.Prepare(
		`UPDATE documents SET docid=?, title=?, body=?, hash=?, file_size=?, modified_at=DATETIME('now', '+8 hours'), updated_at=DATETIME('now', '+8 hours') WHERE id=?`,
	)
	if err != nil {
		return err
	}
	defer updateStmt.Close()

	_, err = updateStmt.Exec(doc.DocId, doc.Title, doc.Body, doc.Hash, doc.FileSize, existingID)
	return err
}

func GetDocumentByDocId(docId string) (*DocumentRecord, error) {
	var doc DocumentRecord

	stmt, err := DB.db.Prepare("SELECT id, docid, collection, path, title, body, hash, file_size, created_at, updated_at FROM documents WHERE docid LIKE ?")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	err = stmt.QueryRow(docId+"%").Scan(&doc.ID, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
		&doc.Hash, &doc.FileSize, &doc.CreatedAt, &doc.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errors.New("document not found")
	}
	if err != nil {
		return nil, err
	}

	countStmt, err := DB.db.Prepare("SELECT COUNT(*) FROM documents WHERE docid LIKE ?")
	if err != nil {
		return &doc, nil
	}
	defer countStmt.Close()

	var count int
	countStmt.QueryRow(docId + "%").Scan(&count)
	if count > 1 {
		return nil, fmt.Errorf("ambiguous docid '%s' matches %d documents, use a longer prefix", docId, count)
	}

	return &doc, nil
}

func GetDocumentByPath(collection, path string) (*DocumentRecord, error) {
	return getDocument("WHERE collection=? AND path=?", collection, path)
}

func GetDocumentByID(id int64) (*DocumentRecord, error) {
	return getDocument("WHERE id=?", id)
}

func getDocument(whereClause string, args ...any) (*DocumentRecord, error) {
	query := "SELECT id, docid, collection, path, title, body, hash, file_size, created_at, updated_at FROM documents " + whereClause
	stmt, err := DB.db.Prepare(query)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	var doc DocumentRecord
	err = stmt.QueryRow(args...).Scan(&doc.ID, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title, &doc.Body,
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
	stmt, err := DB.db.Prepare("DELETE FROM documents WHERE id=?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(id)
	return err
}

func ListDocumentsByCollection(collection string) ([]DocumentRecord, error) {
	stmt, err := DB.db.Prepare(
		"SELECT id, docid, collection, path, title, body, hash, file_size, created_at, updated_at FROM documents WHERE collection=?",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.ID, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title,
			&doc.Body, &doc.Hash, &doc.FileSize, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
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
	stmt, err := DB.db.Prepare("SELECT hash FROM documents WHERE collection=? AND path=?")
	if err != nil {
		return "", err
	}
	defer stmt.Close()

	var hash string
	err = stmt.QueryRow(collection, path).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", errors.New("document not found")
	}
	return hash, err
}

func SearchDocumentsByPath(pathPart string, limit int) ([]DocumentRecord, error) {
	stmt, err := DB.db.Prepare(
		"SELECT id, docid, collection, path, title, body, hash, file_size, created_at, updated_at FROM documents WHERE path LIKE ? ORDER BY path LIMIT ?",
	)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query("%"+pathPart+"%", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var docs []DocumentRecord
	for rows.Next() {
		var doc DocumentRecord
		if err := rows.Scan(&doc.ID, &doc.DocId, &doc.Collection, &doc.Path, &doc.Title,
			&doc.Body, &doc.Hash, &doc.FileSize, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}
	return docs, rows.Err()
}
