package dao

import "database/sql"

var DB *Store

type Store struct {
	db *sql.DB
}

func Init(dbPath string) error {
	var err error
	DB = &Store{}
	DB.db, err = OpenDB(dbPath)
	if err != nil {
		return err
	}
	if err := createTables(); err != nil {
		return err
	}
	return prepareFTSStatements()
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func WithTransaction(fn func(tx *sql.Tx) error) error {
	tx, err := DB.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
