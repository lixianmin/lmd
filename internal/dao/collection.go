package dao

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/lixianmin/logo"
)

type CollectionRecord struct {
	ID             int
	Name           string
	Path           string
	GlobPattern    string
	IgnorePatterns []string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DocCount       int
}

func CollectionExists(name string) bool {
	var count int
	DB.db.QueryRow("SELECT COUNT(1) FROM collections WHERE name=?", name).Scan(&count)
	return count > 0
}

func AddCollection(name, path, globPattern string, ignorePatterns []string) error {
	var ignoreJSON *string
	if len(ignorePatterns) > 0 {
		b, err := json.Marshal(ignorePatterns)
		if err != nil {
			return err
		}
		s := string(b)
		ignoreJSON = &s
	}
	_, err := WithExec("INSERT INTO collections (name, path, glob_pattern, ignore_patterns) VALUES (?, ?, ?, ?)",
		name, path, globPattern, ignoreJSON)
	return err
}

func RemoveCollection(name string) error {
	return withTransaction(func(tx *sql.Tx) error {
		res, err := tx.Exec("DELETE FROM collections WHERE name=?", name)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return errors.New("collection not found: " + name)
		}

		docRows, err := tx.Query("SELECT id FROM documents WHERE collection=?", name)
		if err != nil {
			return err
		}
		var docIds []int64
		for docRows.Next() {
			var id int64
			if err := docRows.Scan(&id); err != nil {
				docRows.Close()
				return err
			}
			docIds = append(docIds, id)
		}
		docRows.Close()
		if err := docRows.Err(); err != nil {
			return err
		}

		if len(docIds) > 0 {
			if err := removeChunksByDocIds(tx, docIds); err != nil {
				return err
			}
			if err := removeDocsByCollection(tx, name); err != nil {
				return err
			}
			if err := removeOrphanSummaries(tx, docIds); err != nil {
				return err
			}
		}

		return nil
	})
}

func removeChunksByDocIds(tx *sql.Tx, docIds []int64) error {
	chunkRows, err := tx.Query(buildInQuery("SELECT id FROM chunks WHERE doc_id IN (", len(docIds), ")"), int64SliceToAny(docIds)...)
	if err != nil {
		return err
	}
	var chunkIds []int64
		for chunkRows.Next() {
			var id int64
			if err := chunkRows.Scan(&id); err != nil {
				chunkRows.Close()
				return err
			}
			chunkIds = append(chunkIds, id)
		}
		chunkRows.Close()
		if err := chunkRows.Err(); err != nil {
			return err
		}

	if len(chunkIds) > 0 {
		if err := execInQuery(tx, "DELETE FROM chunks_vec WHERE chunk_id IN (", chunkIds); err != nil {
			return err
		}
		if err := execInQuery(tx, "DELETE FROM chunks_fts WHERE rowid IN (", chunkIds); err != nil {
			return err
		}
	}

	return execInQuery(tx, "DELETE FROM chunks WHERE doc_id IN (", docIds)
}

func removeDocsByCollection(tx *sql.Tx, name string) error {
	stmt, err := tx.Prepare("DELETE FROM documents WHERE collection=?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(name)
	return err
}

func removeOrphanSummaries(tx *sql.Tx, deletedDocIds []int64) error {
	summaryRows, err := tx.Query(
		buildInQuery("SELECT id FROM documents WHERE collection='@summaries' AND source_doc_id IN (", len(deletedDocIds), ")"),
		int64SliceToAny(deletedDocIds)...,
	)
	if err != nil {
		return err
	}
	var summaryDocIds []int64
	for summaryRows.Next() {
		var id int64
		if err := summaryRows.Scan(&id); err != nil {
			summaryRows.Close()
			return err
		}
		summaryDocIds = append(summaryDocIds, id)
	}
	summaryRows.Close()
	if err := summaryRows.Err(); err != nil {
		return err
	}

	if len(summaryDocIds) > 0 {
		if err := removeChunksByDocIds(tx, summaryDocIds); err != nil {
			return err
		}
		return execInQuery(tx, "DELETE FROM documents WHERE id IN (", summaryDocIds)
	}
	return nil
}

func execInQuery(tx *sql.Tx, prefix string, ids []int64) error {
	stmt, err := tx.Prepare(buildInQuery(prefix, len(ids), ")"))
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(int64SliceToAny(ids)...)
	return err
}

func ListCollections() ([]CollectionRecord, error) {
	rows, err := withQuery(`
		SELECT c.id, c.name, c.path, c.glob_pattern, c.ignore_patterns,
		       c.created_at, c.updated_at,
		       COUNT(d.id) AS doc_count
		FROM collections c
		LEFT JOIN documents d ON d.collection = c.name
		GROUP BY c.id
		ORDER BY c.name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []CollectionRecord
	for rows.Next() {
		var c CollectionRecord
		var ignoreJSON *string
		var docCount int
		if err := rows.Scan(&c.ID, &c.Name, &c.Path, &c.GlobPattern, &ignoreJSON,
			&c.CreatedAt, &c.UpdatedAt, &docCount); err != nil {
			return nil, err
		}
		if ignoreJSON != nil {
			if err := json.Unmarshal([]byte(*ignoreJSON), &c.IgnorePatterns); err != nil {
				return nil, err
			}
		}
		c.DocCount = docCount
		cols = append(cols, c)
	}
	sysRows, err := withQuery(`
		SELECT collection, COUNT(*) as doc_count
		FROM documents
		WHERE collection LIKE '@%'
		GROUP BY collection
	`)
	if err != nil {
		return nil, err
	}
	defer sysRows.Close()

	for sysRows.Next() {
		var name string
		var docCount int
		if err := sysRows.Scan(&name, &docCount); err != nil {
			logo.Error("ListCollections: scan @-collection row failed: %s", err)
			continue
		}
		exists := false
		for _, c := range cols {
			if c.Name == name {
				exists = true
				break
			}
		}
		if !exists {
			cols = append(cols, CollectionRecord{
				Name:     name,
				Path:     "(system)",
				DocCount: docCount,
			})
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := sysRows.Err(); err != nil {
		logo.Error("ListCollections: sysRows iteration error: %s", err)
	}
	return cols, nil
}

func RenameCollection(oldName, newName string) error {
	return withTransaction(func(tx *sql.Tx) error {
		res, err := tx.Exec("UPDATE collections SET name=?, updated_at=DATETIME('now', '+8 hours') WHERE name=?",
			newName, oldName)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return errors.New("collection not found: " + oldName)
		}
		_, err = tx.Exec("UPDATE documents SET collection=? WHERE collection=?", newName, oldName)
		return err
	})
}

func buildInQuery(prefix string, count int, suffix string) string {
	placeholders := make([]string, count)
	for i := range placeholders {
		placeholders[i] = "?"
	}
	return prefix + strings.Join(placeholders, ",") + suffix
}

func int64SliceToAny(ids []int64) []any {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return args
}
