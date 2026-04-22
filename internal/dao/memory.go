package dao

import (
	"database/sql"
	"math"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
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
	query := `
		SELECT m.id, m.content, m.type, abs(f.rank) as raw_score, m.created_at
		FROM memories_fts f
		JOIN memories m ON m.id = f.rowid
		WHERE f.content MATCH ?
		ORDER BY rank LIMIT ?
	`
	rows, err := withQuery(query, tokenizedQuery, limit)
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

func SearchMemoryVector(query []float32, limit int) ([]MemoryRecord, error) {
	q, err := sqlite_vec.SerializeFloat32(padVector(query))
	if err != nil {
		return nil, err
	}

	rows, err := withQuery(`
		SELECT v.memory_id, v.distance
		FROM memories_vec v
		WHERE v.embedding MATCH ?
		ORDER BY v.distance
		LIMIT ?
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []MemoryRecord
	for rows.Next() {
		var id int64
		var distance float64
		if err := rows.Scan(&id, &distance); err != nil {
			return nil, err
		}
		score := 1.0 - distance
		results = append(results, MemoryRecord{ID: id, Score: score})
	}

	for i := range results {
		row := withQueryRow("SELECT content, type, created_at FROM memories WHERE id=?", results[i].ID)
		if err := row.Scan(&results[i].Content, &results[i].Type, &results[i].CreatedAt); err != nil {
			continue
		}
	}

	return results, rows.Err()
}

func UpdateMemoryEmbedding(id int64, vec []byte) error {
	return withTransaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("UPDATE memories SET embedding=? WHERE id=?", vec, id)
		if err != nil {
			return err
		}
		_, err = tx.Exec("INSERT OR REPLACE INTO memories_vec(memory_id, embedding) VALUES (?, ?)", id, vec)
		return err
	})
}

func GetUnembeddedMemoryCount() int {
	var count int
	DB.db.QueryRow(`
		SELECT COUNT(*) FROM memories m
		LEFT JOIN memories_vec v ON m.id = v.memory_id
		WHERE v.memory_id IS NULL
	`).Scan(&count)
	return count
}

func GetUnembeddedMemories(limit int) ([]MemoryRecord, error) {
	rows, err := withQuery(`
		SELECT m.id, m.content, m.type, m.created_at
		FROM memories m
		LEFT JOIN memories_vec v ON m.id = v.memory_id
		WHERE v.memory_id IS NULL
		LIMIT ?
	`, limit)
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
