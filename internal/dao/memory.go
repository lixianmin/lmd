package dao

import (
	"database/sql"
	"math"
	"strings"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/lixianmin/logo"
)

type MemoryRecord struct {
	Id        int64
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
	if err := row.Scan(&rec.Id, &rec.Content, &rec.Type, &embedding, &rec.CreatedAt); err != nil {
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
		if err := rows.Scan(&rec.Id, &rec.Content, &rec.Type, &rawScore, &rec.CreatedAt); err != nil {
			return nil, err
		}
		abs := math.Abs(rawScore)
		rec.Score = abs / (1.0 + abs)
		results = append(results, rec)
	}
	return results, rows.Err()
}

func SearchMemoryVector(q []float32, limit int) ([]MemoryRecord, error) {
	blob, err := sqlite_vec.SerializeFloat32(q)
	if err != nil {
		return nil, err
	}
	rows, err := withQuery(`
		SELECT v.memory_id, v.distance
		FROM memories_vec v
		WHERE v.embedding MATCH ?
		ORDER BY v.distance
		LIMIT ?
	`, blob, limit)
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
		results = append(results, MemoryRecord{Id: id, Score: score})
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return results, nil
	}

	ids := make([]any, len(results))
	placeholders := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.Id
		placeholders[i] = "?"
	}
	query := "SELECT id, content, type, created_at FROM memories WHERE id IN (" +
		strings.Join(placeholders, ",") + ")"
	contentRows, err := withQuery(query, ids...)
	if err != nil {
		return nil, err
	}
	defer contentRows.Close()

	contentMap := make(map[int64]MemoryRecord)
	for contentRows.Next() {
		var rec MemoryRecord
		if err := contentRows.Scan(&rec.Id, &rec.Content, &rec.Type, &rec.CreatedAt); err != nil {
			logo.Warn("SearchMemoryVector: scan content row failed: %s", err)
			continue
		}
		contentMap[rec.Id] = rec
	}

	for i := range results {
		if rec, ok := contentMap[results[i].Id]; ok {
			results[i].Content = rec.Content
			results[i].Type = rec.Type
			results[i].CreatedAt = rec.CreatedAt
		}
	}

	return results, nil
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
	if err := DB.db.QueryRow(`
		SELECT COUNT(*) FROM memories m
		LEFT JOIN memories_vec v ON m.id = v.memory_id
		WHERE v.memory_id IS NULL
	`).Scan(&count); err != nil {
		logo.Error("GetUnembeddedMemoryCount: %s", err)
	}
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
		if err := rows.Scan(&rec.Id, &rec.Content, &rec.Type, &rec.CreatedAt); err != nil {
			return nil, err
		}
		results = append(results, rec)
	}
	return results, rows.Err()
}

func DeleteMemory(id int64) error {
	return withTransaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM memories_vec WHERE memory_id=?", id); err != nil {
			return err
		}
		if _, err := tx.Exec("DELETE FROM memories_fts WHERE rowid=?", id); err != nil {
			return err
		}
		res, err := tx.Exec("DELETE FROM memories WHERE id=?", id)
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
