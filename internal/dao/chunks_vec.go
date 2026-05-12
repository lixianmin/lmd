package dao

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"

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
	Id         int64
	DocId      int64
	Seq        int
	Content    string
	Position   int
	TokenCount int
	Hash       string
}

type VectorSearchResult struct {
	ChunkId  int64
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

func InsertChunks(docId int64, chunks []ChunkData, tokenizedContents []string) ([]ChunkRecord, error) {
	if len(chunks) != len(tokenizedContents) {
		return nil, fmt.Errorf("chunks (%d) and tokenizedContents (%d) must have same length", len(chunks), len(tokenizedContents))
	}

	var records []ChunkRecord
	err := withTransaction(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare("INSERT OR IGNORE INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, ?, ?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer stmt.Close()

		ftsStmt, err := tx.Prepare("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)")
		if err != nil {
			return err
		}
		defer ftsStmt.Close()

		for i, c := range chunks {
			res, err := stmt.Exec(docId, i, c.Content, c.Position, c.TokenCount, c.Hash)
			if err != nil {
				return err
			}
			// INSERT OR IGNORE 跳过冲突行时 RowsAffected = 0
			rows, _ := res.RowsAffected()
			if rows == 0 {
				continue
			}
			id, _ := res.LastInsertId()

			if _, err := ftsStmt.Exec(id, tokenizedContents[i]); err != nil {
				return err
			}

			records = append(records, ChunkRecord{
				Id: id, DocId: docId, Seq: i,
				Content: c.Content, Position: c.Position,
				TokenCount: c.TokenCount, Hash: c.Hash,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

func InsertVector(chunkId, docId int64, collection string, embedding []float32) error {
	vec, err := sqlite_vec.SerializeFloat32(padVector(embedding))
	if err != nil {
		return err
	}
	_, err = WithExec("INSERT INTO chunks_vec(chunk_id, embedding, doc_id, collection) VALUES (?, ?, ?, ?)", chunkId, vec, docId, collection)
	return err
}

func InsertVectors(items []struct {
	ChunkId   int64
	DocId     int64
	Collection string
	Embedding []float32
}) error {
	if len(items) == 0 {
		return nil
	}
	return withTransaction(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare("INSERT INTO chunks_vec(chunk_id, embedding, doc_id, collection) VALUES (?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, item := range items {
			vec, err := sqlite_vec.SerializeFloat32(padVector(item.Embedding))
			if err != nil {
				return err
			}
			if _, err := stmt.Exec(item.ChunkId, vec, item.DocId, item.Collection); err != nil {
				return err
			}
		}
		return nil
	})
}

func InsertChunksAndVectors(docId int64, collection string, startSeq int, chunks []ChunkData, tokenized []string, vecs [][]float32) ([]ChunkRecord, error) {
	if len(chunks) != len(tokenized) || len(chunks) != len(vecs) {
		return nil, fmt.Errorf("chunks(%d), tokenized(%d), vecs(%d) length mismatch", len(chunks), len(tokenized), len(vecs))
	}

	var result []ChunkRecord
	err := withTransaction(func(tx *sql.Tx) error {
		chunkStmt, err := tx.Prepare("INSERT INTO chunks (doc_id, seq, content, position, token_count, hash) VALUES (?, ?, ?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer chunkStmt.Close()

		ftsStmt, err := tx.Prepare("INSERT INTO chunks_fts (rowid, content) VALUES (?, ?)")
		if err != nil {
			return err
		}
		defer ftsStmt.Close()

		vecStmt, err := tx.Prepare("INSERT INTO chunks_vec (chunk_id, embedding, doc_id, collection) VALUES (?, ?, ?, ?)")
		if err != nil {
			return err
		}
		defer vecStmt.Close()

	for i, c := range chunks {
		seq := startSeq + i
		r, err := chunkStmt.Exec(docId, seq, c.Content, c.Position, c.TokenCount, c.Hash)
		if err != nil {
			return err
		}
		rowsAffected, _ := r.RowsAffected()
		if rowsAffected == 0 {
			continue
		}
		chunkId, _ := r.LastInsertId()

		ftsStmt.Exec(chunkId, tokenized[i])

		blob, err := sqlite_vec.SerializeFloat32(padVector(vecs[i]))
		if err != nil {
			return err
		}
		if _, err := vecStmt.Exec(chunkId, blob, docId, collection); err != nil {
			return err
		}

		result = append(result, ChunkRecord{
			Id: chunkId, DocId: docId, Seq: seq,
				Content: c.Content, Position: c.Position,
				TokenCount: c.TokenCount, Hash: c.Hash,
			})
		}
		return nil
	})
	return result, err
}

func DeleteVectorsByDocId(docId int64) error {
	return withTransaction(func(tx *sql.Tx) error {
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

		var chunkIds []int64
		for rows.Next() {
			var chunkId int64
			if err := rows.Scan(&chunkId); err != nil {
				return err
			}
			chunkIds = append(chunkIds, chunkId)
		}
		if err := rows.Err(); err != nil {
			return err
		}

		if len(chunkIds) > 0 {
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

			for _, id := range chunkIds {
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
		_, err = delChunksStmt.Exec(docId)
		return err
	})
}

func QueryVectors(query []float32, limit int) ([]VectorSearchResult, error) {
	q, err := sqlite_vec.SerializeFloat32(padVector(query))
	if err != nil {
		return nil, err
	}

	rows, err := withQuery(`
		SELECT chunk_id, distance
		FROM chunks_vec
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT ?
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var r VectorSearchResult
		if err := rows.Scan(&r.ChunkId, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func QueryVectorsByDocIds(query []float32, docIds []int64, limit int) ([]VectorSearchResult, error) {
	q, err := sqlite_vec.SerializeFloat32(padVector(query))
	if err != nil {
		return nil, err
	}

	placeholders := make([]string, len(docIds))
	args := make([]any, 0, len(docIds)+2)
	args = append(args, q)
	for i, id := range docIds {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, limit)

	queryStr := fmt.Sprintf(`
		SELECT chunk_id, distance
		FROM chunks_vec
		WHERE embedding MATCH ?
		  AND doc_id IN (%s)
		ORDER BY distance
		LIMIT ?
	`, strings.Join(placeholders, ","))

	rows, err := withQuery(queryStr, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var r VectorSearchResult
		if err := rows.Scan(&r.ChunkId, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func QueryVectorsByCollection(query []float32, collection string, limit int) ([]VectorSearchResult, error) {
	q, err := sqlite_vec.SerializeFloat32(padVector(query))
	if err != nil {
		return nil, err
	}

	rows, err := withQuery(`
		SELECT chunk_id, distance
		FROM chunks_vec
		WHERE embedding MATCH ?
		  AND collection = ?
		ORDER BY distance
		LIMIT ?
	`, q, collection, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []VectorSearchResult
	for rows.Next() {
		var r VectorSearchResult
		if err := rows.Scan(&r.ChunkId, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func GetChunksByDocId(docId int64) ([]ChunkRecord, error) {
	rows, err := withQuery("SELECT id, doc_id, seq, content, position, token_count, hash FROM chunks WHERE doc_id=? ORDER BY seq", docId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.Id, &c.DocId, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash); err != nil {
			return nil, err
		}
		chunks = append(chunks, c)
	}
	return chunks, rows.Err()
}

func GetChunkById(chunkId int64) (*ChunkRecord, error) {
	var c ChunkRecord
	err := withQueryRow("SELECT id, doc_id, seq, content, position, token_count, hash FROM chunks WHERE id=?", chunkId).Scan(&c.Id, &c.DocId, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash)
	if err == sql.ErrNoRows {
		return nil, errors.New("chunk not found")
	}
	return &c, err
}

func GetChunksByIds(ids []int64) ([]ChunkRecord, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("SELECT id, doc_id, seq, content, position, token_count, hash FROM chunks WHERE id IN (%s)", strings.Join(placeholders, ","))
	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ChunkRecord
	for rows.Next() {
		var c ChunkRecord
		if err := rows.Scan(&c.Id, &c.DocId, &c.Seq, &c.Content, &c.Position, &c.TokenCount, &c.Hash); err != nil {
			return nil, err
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

func SimilarityToScore(distance float64) float64 {
	return 1.0 - distance
}

type ChunkEmbedding struct {
	ChunkID   int64
	Embedding []float32
}

func GetEmbeddingsByChunkIds(chunkIds []int64) ([]ChunkEmbedding, error) {
	if len(chunkIds) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(chunkIds))
	args := make([]any, len(chunkIds))
	for i, id := range chunkIds {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		"SELECT chunk_id, embedding FROM chunks_vec WHERE chunk_id IN (%s)",
		strings.Join(placeholders, ","),
	)

	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ChunkEmbedding
	for rows.Next() {
		var r ChunkEmbedding
		var vecBlob []byte
		if err := rows.Scan(&r.ChunkID, &vecBlob); err != nil {
			return nil, err
		}
		r.Embedding = deserializeFloat32(vecBlob)
		results = append(results, r)
	}
	return results, rows.Err()
}

func deserializeFloat32(data []byte) []float32 {
	count := len(data) / 4
	vec := make([]float32, count)
	for i := 0; i < count; i++ {
		bits := binary.LittleEndian.Uint32(data[i*4:])
		vec[i] = math.Float32frombits(bits)
	}
	return vec
}
