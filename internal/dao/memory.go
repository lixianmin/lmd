package dao

import (
	"database/sql"
	"math"
	"time"
)

type MemoryRecord struct {
	ID        int64
	Content   string
	Type      string
	Embedding []byte
	Score     float64
	CreatedAt time.Time
}

func InsertMemory(content, memType string) (int64, error) {
	var id int64
	err := withTransaction(func(tx *sql.Tx) error {
		res, err := tx.Exec("INSERT INTO memories (content, type) VALUES (?, ?)", content, memType)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		_, err = tx.Exec("INSERT INTO memories_fts (rowid, content) VALUES (?, ?)", id, content)
		return err
	})
	return id, err
}

func GetMemoryByID(id int64) (*MemoryRecord, error) {
	row := withQueryRow(
		"SELECT id, content, type, embedding, created_at FROM memories WHERE id=?",
		id,
	)
	var rec MemoryRecord
	var embedding []byte
	if err := row.Scan(&rec.ID, &rec.Content, &rec.Type, &embedding, &rec.CreatedAt); err != nil {
		return nil, err
	}
	rec.Embedding = embedding
	return &rec, nil
}

func SearchMemoryFTS(tokenizedQuery string, limit int) ([]MemoryRecord, error) {
	return searchMemoryFTSFiltered(tokenizedQuery, "", limit)
}

func SearchMemoryFTSByType(tokenizedQuery, memType string, limit int) ([]MemoryRecord, error) {
	return searchMemoryFTSFiltered(tokenizedQuery, memType, limit)
}

func searchMemoryFTSFiltered(tokenizedQuery, memType string, limit int) ([]MemoryRecord, error) {
	var query string
	var args []any

	if memType != "" {
		query = `
			SELECT m.id, m.content, m.type, abs(f.rank) as raw_score, m.created_at
			FROM memories_fts f
			JOIN memories m ON m.id = f.rowid
			WHERE f.content MATCH ? AND m.type = ?
			ORDER BY rank LIMIT ?
		`
		args = []any{tokenizedQuery, memType, limit}
	} else {
		query = `
			SELECT m.id, m.content, m.type, abs(f.rank) as raw_score, m.created_at
			FROM memories_fts f
			JOIN memories m ON m.id = f.rowid
			WHERE f.content MATCH ?
			ORDER BY rank LIMIT ?
		`
		args = []any{tokenizedQuery, limit}
	}

	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var rec MemoryRecord
		var rawScore float64
		if err := rows.Scan(&rec.ID, &rec.Content, &rec.Type, &rawScore, &rec.CreatedAt); err != nil {
			return nil, err
		}
		abs := math.Abs(rawScore)
		rec.Score = abs / (1.0 + abs)
		results = append(results, rec)
	}
	return results, rows.Err()
}

func UpdateMemoryEmbedding(id int64, vec []byte) error {
	_, err := WithExec("UPDATE memories SET embedding=? WHERE id=?", vec, id)
	return err
}

func GetUnembeddedMemoryCount() int {
	row := DB.db.QueryRow("SELECT COUNT(*) FROM memories WHERE embedding IS NULL")
	var count int
	row.Scan(&count)
	return count
}

func GetUnembeddedMemories(limit int) ([]MemoryRecord, error) {
	rows, err := withQuery(
		"SELECT id, content, type, created_at FROM memories WHERE embedding IS NULL LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var rec MemoryRecord
		if err := rows.Scan(&rec.ID, &rec.Content, &rec.Type, &rec.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, rec)
	}
	return results, rows.Err()
}
