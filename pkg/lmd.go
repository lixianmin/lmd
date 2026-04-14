package lmd

import (
	"context"
	"fmt"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/service"
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
	DocId      string
	Collection string
	Path       string
	Title      string
	Score      float64
	Snippet    string
	Line       int
}

type Document struct {
	DocId      string
	Collection string
	Path       string
	Title      string
	Body       string
	FileSize   int64
	ModifiedAt time.Time
}

type LmdStore struct {
	tokenizer *tokenizer.GseTokenizer
	indexer   *service.Indexer
	searcher  *service.Searcher
}

func CreateStore(opts StoreOptions) (*LmdStore, error) {
	if err := dao.Init(opts.DBPath); err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	tok, err := tokenizer.NewGseTokenizer()
	if err != nil {
		dao.DB.Close()
		return nil, fmt.Errorf("failed to initialize tokenizer: %w", err)
	}

	return &LmdStore{
		tokenizer: tok,
		indexer:   service.NewIndexer(tok),
		searcher:  service.NewSearcher(tok),
	}, nil
}

func (s *LmdStore) AddCollection(name string, config CollectionConfig) error {
	glob := config.GlobPattern
	if glob == "" {
		glob = "**/*.md"
	}
	return dao.AddCollection(name, config.Path, glob, config.IgnorePatterns)
}

func (s *LmdStore) RemoveCollection(name string) error {
	return dao.RemoveCollection(name)
}

func (s *LmdStore) ListCollections() ([]CollectionInfo, error) {
	cols, err := dao.ListCollections()
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
	cols, err := dao.ListCollections()
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
			DocId:      h.DocId,
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

func (s *LmdStore) Get(pathOrDocId string) (*Document, error) {
	var doc *dao.DocumentRecord
	var err error

	if len(pathOrDocId) > 0 && pathOrDocId[0] == '#' {
		doc, err = dao.GetDocumentByDocId(pathOrDocId[1:])
	} else {
		parts := splitPath(pathOrDocId)
		if len(parts) == 2 {
			doc, err = dao.GetDocumentByPath(parts[0], parts[1])
		} else {
			return nil, fmt.Errorf("invalid path format, use collection/path or #docid")
		}
	}
	if err != nil {
		return nil, err
	}

	return &Document{
		DocId:      doc.DocId,
		Collection: doc.Collection,
		Path:       doc.Path,
		Title:      doc.Title,
		Body:       doc.Body,
		FileSize:   doc.FileSize,
		ModifiedAt: doc.ModifiedAt,
	}, nil
}

func (s *LmdStore) Close() error {
	return dao.DB.Close()
}

func splitPath(p string) []string {
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return []string{p[:i], p[i+1:]}
		}
	}
	return []string{p}
}
