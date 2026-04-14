package dao

import (
	"database/sql"
	"math"
)

type FTSSearchResult struct {
	ChunkID    int64
	DocId      string
	Collection string
	Path       string
	Title      string
	Content    string
	Score      float64
}

var ftsSearchAll *sql.Stmt
var ftsSearchByCollection *sql.Stmt

func prepareFTSStatements() error {
	var err error
	ftsSearchAll, err = DB.db.Prepare(`
		SELECT c.id, d.docid, d.collection, d.path, d.title, c.content,
			   abs(rank) as raw_score
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
		SELECT c.id, d.docid, d.collection, d.path, d.title, c.content,
			   abs(rank) as raw_score
		FROM chunks_fts f
		JOIN chunks c ON c.id = f.rowid
		JOIN documents d ON d.id = c.doc_id
		WHERE f.content MATCH ? AND d.collection = ?
		ORDER BY rank LIMIT ?
	`)
	return err
}

func SearchFTS(tokenizedQuery, collection string, limit int) ([]FTSSearchResult, error) {
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
		if err := rows.Scan(&r.ChunkID, &r.DocId, &r.Collection, &r.Path, &r.Title, &r.Content, &r.Score); err != nil {
			return nil, err
		}
		abs := math.Abs(r.Score)
		r.Score = abs / (1.0 + abs)
		results = append(results, r)
	}

	return results, rows.Err()
}
