package dao

func GetChunkCounts() (total int, embedded int) {
	DB.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&total)
	DB.db.QueryRow("SELECT COUNT(*) FROM chunks_vec_rowids").Scan(&embedded)
	return
}

func GetUnembeddedCount() int {
	var count int
	DB.db.QueryRow(`
		SELECT COUNT(*) FROM chunks c
		LEFT JOIN chunks_vec v ON c.id = v.chunk_id
		WHERE v.chunk_id IS NULL
	`).Scan(&count)
	return count
}
