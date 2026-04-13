package store

import (
	"database/sql"
	"math"
)

type FTSSearchResult struct {
	ID         int64
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
}

var ftsSearchAll *sql.Stmt
var ftsSearchByCollection *sql.Stmt

func PrepareFTSStatements(db *sql.DB) error {
	var err error
	ftsSearchAll, err = db.Prepare(`
		SELECT d.id, d.docid, d.collection, d.path, d.title,
			   abs(rank) as raw_score
		FROM documents_fts f
		JOIN documents d ON d.id = f.rowid
		WHERE f.tokens MATCH ?
		ORDER BY rank LIMIT ?
	`)
	if err != nil {
		return err
	}

	ftsSearchByCollection, err = db.Prepare(`
		SELECT d.id, d.docid, d.collection, d.path, d.title,
			   abs(rank) as raw_score
		FROM documents_fts f
		JOIN documents d ON d.id = f.rowid
		WHERE f.tokens MATCH ? AND d.collection = ?
		ORDER BY rank LIMIT ?
	`)
	return err
}

func SearchFTS(db *sql.DB, tokenizedQuery, collection string, limit int) ([]FTSSearchResult, error) {
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
		if err := rows.Scan(&r.ID, &r.DocID, &r.Collection, &r.Path, &r.Title, &r.Score); err != nil {
			return nil, err
		}
		results = append(results, r)
	}

	if len(results) > 0 {
		topScore := results[0].Score
		for i := range results {
			results[i].Score = math.Min(results[i].Score/topScore, 1.0)
		}
	}

	return results, rows.Err()
}
