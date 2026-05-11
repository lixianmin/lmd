package dao

import "github.com/lixianmin/logo"

func GetChunkCounts() (total int, embedded int) {
	if DB == nil || DB.db == nil {
		return
	}
	if err := DB.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&total); err != nil {
		logo.Error("GetChunkCounts: %s", err)
	}
	if err := DB.db.QueryRow("SELECT COUNT(*) FROM chunks_vec_rowids").Scan(&embedded); err != nil {
		logo.Error("GetChunkCounts: embedded %s", err)
	}
	return
}

func GetChunkCountsByCollection() map[string]int {
	if DB == nil || DB.db == nil {
		return nil
	}
	rows, err := DB.db.Query(`
		SELECT d.collection, COUNT(c.id)
		FROM documents d
		JOIN chunks c ON c.doc_id = d.id
		GROUP BY d.collection
	`)
	if err != nil {
		logo.Error("GetChunkCountsByCollection: %s", err)
		return nil
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			logo.Error("GetChunkCountsByCollection: scan %s", err)
			continue
		}
		result[name] = count
	}
	if err := rows.Err(); err != nil {
		logo.Error("GetChunkCountsByCollection: iter %s", err)
	}
	return result
}

func GetSummaryCounts() (totalDocs int, summaries int) {
	if DB == nil || DB.db == nil {
		return
	}
	DB.db.QueryRow("SELECT COUNT(*) FROM documents WHERE collection != '@summaries'").Scan(&totalDocs)
	DB.db.QueryRow("SELECT COUNT(*) FROM documents WHERE collection = '@summaries'").Scan(&summaries)
	return
}

func GetUnembeddedCount() int {
	if DB == nil || DB.db == nil {
		return 0
	}
	var count int
	if err := DB.db.QueryRow(`
		SELECT COUNT(*) FROM chunks c
		LEFT JOIN chunks_vec v ON c.id = v.chunk_id
		WHERE v.chunk_id IS NULL
	`).Scan(&count); err != nil {
		logo.Error("GetUnembeddedCount: %s", err)
	}
	return count
}
