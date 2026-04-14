package store

import (
	"database/sql"
	"errors"
	"fmt"

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
	DocId      int64
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

func InsertChunks(db *sql.DB, docId int64, chunks []ChunkData, tokenizedContents []string) ([]ChunkRecord, error) {
	if len(chunks) != len(tokenizedContents) {
		return nil, fmt.Errorf("chunks (%d) and tokenizedContents (%d) must have same length", len(chunks), len(tokenizedContents))
	}

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
		res, err := stmt.Exec(docId, i, c.Content, c.Position, c.TokenCount, c.Hash)
		if err != nil {
			return nil, err
		}
		id, _ := res.LastInsertId()

		if _, err := ftsStmt.Exec(id, tokenizedContents[i]); err != nil {
			return nil, err
		}

		records = append(records, ChunkRecord{
			ID: id, DocId: docId, Seq: i,
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

func InsertVector(db *sql.DB, chunkId int64, embedding []float32) error {
	vec, err := sqlite_vec.SerializeFloat32(padVector(embedding))
	if err != nil {
		return err
	}
	stmt, err := db.Prepare("INSERT INTO chunks_vec(chunk_id, embedding) VALUES (?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(chunkId, vec)
	return err
}

func DeleteVectorsByDocId(db *sql.DB, docId int64) error {
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

	rows, err := selectStmt.Query(docId)
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
	if _, err := delChunksStmt.Exec(docId); err != nil {
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

func GetUnembeddedChunks(db *sql.DB) ([]ChunkRecord, error) {
	stmt, err := db.Prepare(`
		SELECT c.id, c.doc_id, c.seq, c.content, c.position, c.token_count, c.hash
		FROM chunks c
		LEFT JOIN chunks_vec v ON c.id = v.chunk_id
		WHERE v.chunk_id IS NULL
		ORDER BY c.id
	`)
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.DocId, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func GetChunksByDocId(db *sql.DB, docId int64) ([]ChunkRecord, error) {
	stmt, err := db.Prepare("SELECT id, doc_id, seq, content, position, token_count, hash FROM chunks WHERE doc_id=? ORDER BY seq")
	if err != nil {
		return nil, err
	}
	defer stmt.Close()

	rows, err := stmt.Query(docId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.ID, &c.DocId, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash); err != nil {
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
	err = stmt.QueryRow(chunkID).Scan(&c.ID, &c.DocId, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash)
	if err == sql.ErrNoRows {
		return nil, errors.New("chunk not found")
	}
	return &c, err
}

func SimilarityToScore(distance float64) float64 {
	return 1.0 - distance
}
