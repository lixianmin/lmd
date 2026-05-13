package dao

import (
	"database/sql"
	"encoding/json"
)

func insertDocumentsLog(tx *sql.Tx, docId int64, operation string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO documents_log (doc_id, operation, data_json) VALUES (?, ?, ?)",
		docId, operation, string(jsonData))
	return err
}

func insertChunksLog(tx *sql.Tx, chunkId, docId int64, operation string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO chunks_log (chunk_id, doc_id, operation, data_json) VALUES (?, ?, ?, ?)",
		chunkId, docId, operation, string(jsonData))
	return err
}
