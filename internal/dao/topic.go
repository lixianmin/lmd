package dao

import (
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

type TopicRecord struct {
	Collection string
	RelPath    string
	Overview   string
	DocPaths   string
	Hash       string
	UpdatedAt  string
}

func UpsertTopic(collection, relPath, overview, docPaths, hash string) error {
	_, err := WithExec(`
		INSERT INTO topics (collection, rel_path, overview, doc_paths, hash)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(collection, rel_path) DO UPDATE SET
			overview=excluded.overview,
			doc_paths=excluded.doc_paths,
			hash=excluded.hash,
			updated_at=DATETIME('now', '+8 hours')
	`, collection, relPath, overview, docPaths, hash)
	return err
}

func GetTopic(collection, relPath string) (*TopicRecord, error) {
	var t TopicRecord
	err := withQueryRow(
		"SELECT collection, rel_path, overview, doc_paths, hash, updated_at FROM topics WHERE collection=? AND rel_path=?",
		collection, relPath,
	).Scan(&t.Collection, &t.RelPath, &t.Overview, &t.DocPaths, &t.Hash, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("topic not found: %s/%s: %w", collection, relPath, err)
	}
	return &t, nil
}

func ListTopicsByCollection(collection string) ([]TopicRecord, error) {
	rows, err := withQuery(
		"SELECT collection, rel_path, overview, doc_paths, hash, updated_at FROM topics WHERE collection=? ORDER BY rel_path",
		collection,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []TopicRecord
	for rows.Next() {
		var t TopicRecord
		if err := rows.Scan(&t.Collection, &t.RelPath, &t.Overview, &t.DocPaths, &t.Hash, &t.UpdatedAt); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

func ListAllTopics() ([]TopicRecord, error) {
	rows, err := withQuery(
		"SELECT collection, rel_path, overview, doc_paths, hash, updated_at FROM topics ORDER BY collection, rel_path",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var topics []TopicRecord
	for rows.Next() {
		var t TopicRecord
		if err := rows.Scan(&t.Collection, &t.RelPath, &t.Overview, &t.DocPaths, &t.Hash, &t.UpdatedAt); err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

func DeleteTopic(collection, relPath string) error {
	_, err := WithExec("DELETE FROM topics WHERE collection=? AND rel_path=?", collection, relPath)
	return err
}

func DeleteTopicsByCollection(collection string) error {
	_, err := WithExec("DELETE FROM topics WHERE collection=?", collection)
	return err
}

func GetTopicRowID(collection, relPath string) (int64, error) {
	var rowID int64
	err := withQueryRow(
		"SELECT rowid FROM topics WHERE collection=? AND rel_path=?",
		collection, relPath,
	).Scan(&rowID)
	return rowID, err
}

func GetTopicByRowID(rowID int64) (*TopicRecord, error) {
	var t TopicRecord
	err := withQueryRow(
		"SELECT collection, rel_path, overview, doc_paths, hash, updated_at FROM topics WHERE rowid=?",
		rowID,
	).Scan(&t.Collection, &t.RelPath, &t.Overview, &t.DocPaths, &t.Hash, &t.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("topic not found for rowid %d: %w", rowID, err)
	}
	return &t, nil
}

func UpsertTopicVector(topicRowID int64, embedding []float32) error {
	padded := padVector(embedding)
	vec, err := sqlite_vec.SerializeFloat32(padded)
	if err != nil {
		return err
	}
	_, err = WithExec(
		"INSERT INTO topics_vec(topic_rowid, overview_vector) VALUES (?, ?)",
		topicRowID, vec,
	)
	return err
}

type TopicVectorResult struct {
	TopicRowID int64
	Distance   float64
}

func QueryTopicVectors(query []float32, limit int) ([]TopicVectorResult, error) {
	q, err := sqlite_vec.SerializeFloat32(padVector(query))
	if err != nil {
		return nil, err
	}

	rows, err := withQuery(`
		SELECT topic_rowid, distance
		FROM topics_vec
		WHERE overview_vector MATCH ?
		ORDER BY distance
		LIMIT ?
	`, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []TopicVectorResult
	for rows.Next() {
		var r TopicVectorResult
		if err := rows.Scan(&r.TopicRowID, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

func DeleteTopicVectorByRowID(rowID int64) error {
	_, err := WithExec("DELETE FROM topics_vec WHERE topic_rowid=?", rowID)
	return err
}
