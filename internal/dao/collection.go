package dao

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
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

		if len(docIds) > 0 {
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

			if len(chunkIds) > 0 {
				delVecStmt, err := tx.Prepare(buildInQuery("DELETE FROM chunks_vec WHERE chunk_id IN (", len(chunkIds), ")"))
				if err != nil {
					return err
				}
				if _, err := delVecStmt.Exec(int64SliceToAny(chunkIds)...); err != nil {
					delVecStmt.Close()
					return err
				}
				delVecStmt.Close()

				delFtsStmt, err := tx.Prepare(buildInQuery("DELETE FROM chunks_fts WHERE rowid IN (", len(chunkIds), ")"))
				if err != nil {
					return err
				}
				if _, err := delFtsStmt.Exec(int64SliceToAny(chunkIds)...); err != nil {
					delFtsStmt.Close()
					return err
				}
				delFtsStmt.Close()
			}

			delChunksStmt, err := tx.Prepare(buildInQuery("DELETE FROM chunks WHERE doc_id IN (", len(docIds), ")"))
			if err != nil {
				return err
			}
			if _, err := delChunksStmt.Exec(int64SliceToAny(docIds)...); err != nil {
				delChunksStmt.Close()
				return err
			}
			delChunksStmt.Close()

			delDocsStmt, err := tx.Prepare("DELETE FROM documents WHERE collection=?")
			if err != nil {
				return err
			}
			if _, err := delDocsStmt.Exec(name); err != nil {
				delDocsStmt.Close()
				return err
			}
			delDocsStmt.Close()
		}

		return nil
	})
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
	return cols, rows.Err()
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
