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
