package store

import (
	"database/sql"
	"errors"
	"math"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

const EmbeddingDim = 1024

type ChunkData struct {
	Content    string
	Position   int
	TokenCount int
	Hash       string
}

type ChunkRecord struct {
	ID         int64
	DocID      int64
	Seq        int
	Content    string
	Position   int
	TokenCount int
	Hash       string
}

type VectorSearchResult struct {
	ChunkID  int64
	Distance float64
}

func padVector(vec []float32) []float32 {
	if len(vec) == EmbeddingDim {
		return vec
	}
	padded := make([]float32, EmbeddingDim)
	copy(padded, vec)
	return padded
}

func InsertChunks(db *sql.DB, docID int64, chunks []ChunkData, tokenizedContents []string) ([]ChunkRecord, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	commit := false
	defer func() {
		if !commit {
			tx.Rollback()
		}
	}()

	stmt, err := tx.Prepare("INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	ftsStmt, err := tx.Prepare("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)")
	if err != nil {
		return nil, err
	}
	defer ftsStmt.Close()

	var records []ChunkRecord
	for i, c := range chunks {
		res, err := stmt.Exec(docID, i, c.Content, c.Position, c.TokenCount, c.Hash)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()

		tokenized := c.Content
		if tokenizedContents != nil && i < len(tokenizedContents) {
			tokenized = tokenizedContents[i]
		}
		if _, err := ftsStmt.Exec(id, tokenized); err != nil {
			return nil, err
		}

		records = append(records, ChunkRecord{
			ID: id, DocID: docID, Seq: i,
			Content: c.Content, Position: c.Position,
			TokenCount: c.TokenCount, Hash: c.Hash,
		})
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	commit = true
	return records, nil
}

func InsertVector(db *sql.DB, chunkID int64, embedding []float32) error {
	vec, err := sqlite_vec.SerializeFloat32(padVector(embedding))
	if err != nil {
		return err
	}
	stmt, err := db.Prepare("INSERT INTO chunks_vec(chunk_id, embedding) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(chunkID, vec)
	return err
}

func DeleteVectorsByDocID(db *sql.DB, docID int64) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	commit := false
	defer func() {
		if !commit {
			tx.Rollback()
		}
	}()

	selectStmt, err := tx.Prepare("SELECT id FROM chunks WHERE doc_id=?")
	if err != nil {
		return err
	}
	defer selectStmt.Close()

	rows, err := selectStmt.Query(docID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var chunkIDs []int64
	for rows.Next() {
		var chunkID int64
		rows.Scan(&chunkID)
		chunkIDs = append(chunkIDs, chunkID)
	}

	if len(chunkIDs) > 0 {
		delVecStmt, err := tx.Prepare("DELETE FROM chunks_vec WHERE chunk_id=?")
		if err != nil {
			return err
		}
		defer delVecStmt.Close()

		delFtsStmt, err := tx.Prepare("DELETE FROM chunks_fts WHERE rowid=?")
		if err != nil {
			return err
		}
		defer delFtsStmt.Close()

		for _, id := range chunkIDs {
			if _, err := delVecStmt.Exec(id); err != nil {
				return err
			}
			if _, err := delFtsStmt.Exec(id); err != nil {
				return err
			}
		}
	}

	delChunksStmt, err := tx.Prepare("DELETE FROM chunks WHERE doc_id=?")
	if err != nil {
		return err
	}
	defer delChunksStmt.Close()
	if _, err := delChunksStmt.Exec(docID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	commit = true
	return nil
}

func QueryVectors(db *sql.DB, query []float32, limit int) ([]VectorSearchResult, error) {
	q, err := sqlite_vec.SerializeFloat32(padVector(query))
	if err != nil {
		return nil, err
	}

	stmt, err := db.Prepare(`
		SELECT chunk_id, distance
		FROM chunks_vec
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT ?
	`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var r VectorSearchResult
		if err := rows.Scan(&r.ChunkID, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func GetUnembeddedChunks(db *sql.DB, modelName string) ([]ChunkRecord, error) {
	stmt, err := db.Prepare(`
		SELECT c.id, c.doc_id, c.seq, c.content, c.position, c.token_count, c.hash
		FROM chunks c
		WHERE c.id NOT IN (
			SELECT chunk_id FROM embed_status WHERE model_name = ?
		)
		ORDER BY c.id
	`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(modelName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.DocID, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func MarkEmbedded(db *sql.DB, chunkID int64, modelName string) error {
	stmt, err := db.Prepare("INSERT OR IGNORE INTO embed_status (chunk_id, model_name) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(chunkID, modelName)
	return err
}

func GetChunksByDocID(db *sql.DB, docID int64) ([]ChunkRecord, error) {
	stmt, err := db.Prepare("SELECT id, doc_id, seq, content, position, token_count, hash FROM chunks WHERE doc_id=? ORDER BY seq")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(docID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.DocID, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func GetChunkByID(db *sql.DB, chunkID int64) (*ChunkRecord, error) {
	stmt, err := db.Prepare("SELECT id, doc_id, seq, content, position, token_count, hash FROM chunks WHERE id=?")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	var c ChunkRecord
	err = stmt.QueryRow(chunkID).Scan(&c.ID, &c.DocID, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash)
	if err == sql.ErrNoRows {
		return nil, errors.New("chunk not found")
	}
	return &c, err
}

func CountEmbedded(db *sql.DB, modelName string) (int, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM embed_status WHERE model_name=?", modelName).Scan(&count)
	return count, err
}

func SimilarityToScore(distance float64) float64 {
	return 1.0 / (1.0 + distance)
}

func NormalizeScore(score, maxScore float64) float64 {
	if maxScore == 0 {
		return 0
	}
	return math.Min(score/maxScore, 1.0)
}
