package dao

import (
	"encoding/json"
	"errors"
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

	stmt, err := DB.db.Prepare("INSERT INTO collections (name, path, glob_pattern, ignore_patterns) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()
	_, err = stmt.Exec(name, path, globPattern, ignoreJSON)
	return err
}

func RemoveCollection(name string) error {
	stmt, err := DB.db.Prepare("DELETE FROM collections WHERE name=?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("collection not found: " + name)
	}
	return nil
}

func ListCollections() ([]CollectionRecord, error) {
	stmt, err := DB.db.Prepare(`
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
	defer stmt.Close()

	rows, err := stmt.Query()
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
			json.Unmarshal([]byte(*ignoreJSON), &c.IgnorePatterns)
		}
		c.DocCount = docCount
		cols = append(cols, c)
	}
	return cols, rows.Err()
}

func RenameCollection(oldName, newName string) error {
	stmt, err := DB.db.Prepare("UPDATE collections SET name=?, updated_at=DATETIME('now', '+8 hours') WHERE name=?")
	if err != nil {
		return err
	}
	defer stmt.Close()

	res, err := stmt.Exec(newName, oldName)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("collection not found: " + oldName)
	}
	return nil
}
