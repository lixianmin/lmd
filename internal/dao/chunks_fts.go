package dao

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
)

type FTSSearchResult struct {
	ChunkID    int64
	DocRowId   int64
	DocId      string
	Collection string
	Path       string
	Title      string
	Content    string
	Score      float64
	Line       int
}

var ftsSearchAll *sql.Stmt
var ftsSearchByCollection *sql.Stmt

func prepareFTSStatements() error {
	closeFTSStatements()

	var err error
	ftsSearchAll, err = DB.db.Prepare(`
		SELECT c.id, d.id, d.doc_id, d.collection, d.path, d.title, c.content,
			   abs(rank) as raw_score, c.position
		FROM chunks_fts f
		JOIN chunks c ON c.id = f.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE f.content MATCH ?
		ORDER BY rank LIMIT ?
	`)
	if err != nil {
		return err
	}

	ftsSearchByCollection, err = DB.db.Prepare(`
		SELECT c.id, d.id, d.doc_id, d.collection, d.path, d.title, c.content,
			   abs(rank) as raw_score, c.position
		FROM chunks_fts f
		JOIN chunks c ON c.id = f.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE f.content MATCH ? AND d.collection = ?
		ORDER BY rank LIMIT ?
	`)
	return err
}

func closeFTSStatements() {
	if ftsSearchAll != nil {
		ftsSearchAll.Close()
		ftsSearchAll = nil
	}
	if ftsSearchByCollection != nil {
		ftsSearchByCollection.Close()
		ftsSearchByCollection = nil
	}
}

func CloseFTSStatements() {
	closeFTSStatements()
}

func SearchFTS(tokenizedQuery string, collection string, limit int) ([]FTSSearchResult, error) {
	var rows *sql.Rows
	var err error

	if collection != "" {
		rows, err = ftsSearchByCollection.Query(tokenizedQuery, collection, limit)
	} else {
		rows, err = ftsSearchAll.Query(tokenizedQuery, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FTSSearchResult
	for rows.Next() {
		var r FTSSearchResult
		if err := rows.Scan(&r.ChunkID, &r.DocRowId, &r.DocId, &r.Collection, &r.Path, &r.Title, &r.Content, &r.Score, &r.Line); err != nil {
			return nil, err
		}
		abs := math.Abs(r.Score)
		r.Score = abs / (1.0 + abs)
		results = append(results, r)
	}

	return results, rows.Err()
}

func SearchFTSByDocIds(tokenizedQuery string, docIds []int64, limit int) ([]FTSSearchResult, error) {
	placeholders := make([]string, len(docIds))
	args := make([]any, 0, len(docIds)+2)
	args = append(args, tokenizedQuery)
	for i, id := range docIds {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, limit)

	query := fmt.Sprintf(`SELECT c.id, d.id, d.doc_id, d.collection, d.path, d.title, c.content,
		abs(rank) as raw_score, c.position
		FROM chunks_fts f
		JOIN chunks c ON c.id = f.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE f.content MATCH ? AND c.doc_id IN (%s)
		ORDER BY rank LIMIT ?`, strings.Join(placeholders, ","))

	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FTSSearchResult
	for rows.Next() {
		var r FTSSearchResult
		if err := rows.Scan(&r.ChunkID, &r.DocRowId, &r.DocId, &r.Collection, &r.Path, &r.Title, &r.Content, &r.Score, &r.Line); err != nil {
			return nil, err
		}
		abs := math.Abs(r.Score)
		r.Score = abs / (1.0 + abs)
		results = append(results, r)
	}
	return results, rows.Err()
}

func SearchFTSBM25(tokenizedQuery string, collection string, limit int) ([]FTSSearchResult, error) {
	var query string
	var args []any
	if collection != "" {
		query = `SELECT c.id, d.id, d.doc_id, d.collection, d.path, d.title, c.content,
			abs(bm25(chunks_fts, 1.5, 4.0, 1.0)) as raw_score, c.position
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ? AND d.collection = ?
		ORDER BY rank LIMIT ?`
		args = []any{tokenizedQuery, collection, limit}
	} else {
		query = `SELECT c.id, d.id, d.doc_id, d.collection, d.path, d.title, c.content,
			abs(bm25(chunks_fts, 1.5, 4.0, 1.0)) as raw_score, c.position
		FROM chunks_fts
		JOIN chunks c ON c.id = chunks_fts.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE chunks_fts MATCH ?
		ORDER BY rank LIMIT ?`
		args = []any{tokenizedQuery, limit}
	}

	rows, err := withQuery(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []FTSSearchResult
	for rows.Next() {
		var r FTSSearchResult
		if err := rows.Scan(&r.ChunkID, &r.DocRowId, &r.DocId, &r.Collection, &r.Path, &r.Title, &r.Content, &r.Score, &r.Line); err != nil {
			return nil, err
		}
		abs := math.Abs(r.Score)
		r.Score = abs / (1.0 + abs)
		results = append(results, r)
	}

	return results, rows.Err()
}
