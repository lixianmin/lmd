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

func SearchFTS(db *sql.DB, tokenizedQuery, collection string, limit int) ([]FTSSearchResult, error) {
	query := `
		SELECT d.id, d.docid, d.collection, d.path, d.title,
			   abs(rank) as raw_score
		FROM documents_fts f
		JOIN documents d ON d.id = f.rowid
		WHERE f.tokens MATCH ?
	`
	args := []any{tokenizedQuery}

	if collection != "" {
		query += " AND d.collection = ?"
		args = append(args, collection)
	}

	query += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(query, args...)
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
