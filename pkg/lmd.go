package lmd

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

type CollectionConfig struct {
	Path           string
	GlobPattern    string
	IgnorePatterns []string
}

type CollectionInfo struct {
	Name        string
	Path        string
	GlobPattern string
	DocCount    int
}

type StoreOptions struct {
	DBPath string
}

type UpdateOptions struct {
	Collections []string
}

type UpdateResult = service.UpdateResult

type LexOptions struct {
	Collection string
	Limit      int
	MinScore   float64
}

type SearchResult struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
}

type Document struct {
	DocID      string
	Collection string
	Path       string
	Title      string
	Body       string
	FileSize   int64
	ModifiedAt time.Time
}

type LmdStore struct {
	db        *sql.DB
	tokenizer *tokenizer.GseTokenizer
	indexer   *service.Indexer
	searcher  *service.Searcher
}

func CreateStore(opts StoreOptions) (*LmdStore, error) {
	db, err := store.OpenAndInit(opts.DBPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize tokenizer: %w", err)
	}

	return &LmdStore{
		db:        db,
		tokenizer: tok,
		indexer:   service.NewIndexer(db, tok),
		searcher:  service.NewSearcher(db, tok),
	}, nil
}

func (s *LmdStore) AddCollection(name string, config CollectionConfig) error {
	glob := config.GlobPattern
	if glob == "" {
		glob = "**/*.md"
	}
	return store.AddCollection(s.db, name, config.Path, glob, config.IgnorePatterns)
}

func (s *LmdStore) RemoveCollection(name string) error {
	return store.RemoveCollection(s.db, name)
}

func (s *LmdStore) ListCollections() ([]CollectionInfo, error) {
	cols, err := store.ListCollections(s.db)
	if err != nil {
		return nil, err
	}
	result := make([]CollectionInfo, len(cols))
	for i, c := range cols {
		result[i] = CollectionInfo{
			Name:        c.Name,
			Path:        c.Path,
			GlobPattern: c.GlobPattern,
			DocCount:    c.DocCount,
		}
	}
	return result, nil
}

func (s *LmdStore) Update(ctx context.Context, opts UpdateOptions) (*UpdateResult, error) {
	cols, err := store.ListCollections(s.db)
	if err != nil {
		return nil, err
	}

	total := &UpdateResult{}
	for _, col := range cols {
		if len(opts.Collections) > 0 {
			found := false
			for _, name := range opts.Collections {
				if name == col.Name {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}

		result, err := s.indexer.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			return nil, err
		}
		total.Indexed += result.Indexed
		total.Updated += result.Updated
		total.Unchanged += result.Unchanged
		total.Removed += result.Removed
	}
	return total, nil
}

func (s *LmdStore) SearchLex(query string, opts LexOptions) ([]SearchResult, error) {
	limit := opts.Limit
	if limit == 0 {
		limit = 10
	}
	hits, err := s.searcher.SearchLex(query, opts.Collection, limit, opts.MinScore)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, len(hits))
	for i, h := range hits {
		results[i] = SearchResult{
			DocID:      h.DocID,
			Collection: h.Collection,
			Path:       h.Path,
			Title:      h.Title,
			Score:      h.Score,
			Snippet:    h.Snippet,
			Line:       h.Line,
		}
	}
	return results, nil
}

func (s *LmdStore) Get(pathOrDocID string) (*Document, error) {
	var doc *store.DocumentRecord
	var err error

	if len(pathOrDocID) > 0 && pathOrDocID[0] == '#' {
		doc, err = store.GetDocumentByDocID(s.db, pathOrDocID[1:])
	} else {
		parts := splitPath(pathOrDocID)
		if len(parts) == 2 {
			doc, err = store.GetDocumentByPath(s.db, parts[0], parts[1])
		} else {
			return nil, fmt.Errorf("invalid path format, use collection/path or #docid")
		}
	}
	if err != nil {
		return nil, err
	}

	return &Document{
		DocID:      doc.DocID,
		Collection: doc.Collection,
		Path:       doc.Path,
		Title:      doc.Title,
		Body:       doc.Body,
		FileSize:   doc.FileSize,
		ModifiedAt: doc.ModifiedAt,
	}, nil
}

func (s *LmdStore) Close() error {
	return s.db.Close()
}

func splitPath(p string) []string {
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return []string{p[:i], p[i+1:]}
		}
	}
	return []string{p}
}
