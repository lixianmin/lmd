package dao

import (
	"database/sql"
	"errors"
	"strings"
)

type ContextRecord struct {
	Collection string
	Path       string
	Context    string
}

func AddContext(collection, p, context string) error {
	_, err := withExec("INSERT OR REPLACE INTO path_contexts (collection, path, context) VALUES (?, ?, ?)", collection, p, context)
	return err
}

func GetContext(collection, p string) (string, error) {
	var ctx string
	err := withQueryRow("SELECT context FROM path_contexts WHERE collection=? AND path=?", collection, p).Scan(&ctx)
	if err == sql.ErrNoRows {
		return "", errors.New("context not found")
	}
	return ctx, err
}

func RemoveContext(collection, p string) error {
	res, err := withExec("DELETE FROM path_contexts WHERE collection=? AND path=?", collection, p)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("context not found")
	}
	return nil
}

func ListContexts(collection string) ([]ContextRecord, error) {
	rows, err := withQuery("SELECT collection, path, context FROM path_contexts WHERE collection=? ORDER BY path", collection)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contexts []ContextRecord
	for rows.Next() {
		var c ContextRecord
		if err := rows.Scan(&c.Collection, &c.Path, &c.Context); err != nil {
			return nil, err
		}
		contexts = append(contexts, c)
	}
	return contexts, rows.Err()
}

func FindBestContext(collection, docPath string) string {
	parts := strings.Split(docPath, "/")
	for i := len(parts); i >= 0; i-- {
		p := strings.Join(parts[:i], "/")
		ctx, err := GetContext(collection, p)
		if err == nil && ctx != "" {
			return ctx
		}
	}
	return ""
}
