package dao

import "database/sql"

func SetMeta(key, value string) error {
	_, err := WithExec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", key, value)
	return err
}

func GetMeta(key string) (string, bool, error) {
	var value string
	err := withQueryRow("SELECT value FROM meta WHERE key=?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

func DeleteMeta(key string) error {
	_, err := WithExec("DELETE FROM meta WHERE key=?", key)
	return err
}
