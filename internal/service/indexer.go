package service

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lixianmin/lmd/internal/chunker"
	"github.com/lixianmin/lmd/internal/store"
	"github.com/lixianmin/lmd/internal/tokenizer"
)

type UpdateResult struct {
	Indexed   int
	Updated   int
	Unchanged int
	Removed   int
}

type Indexer struct {
	db        *sql.DB
	tokenizer tokenizer.Tokenizer
	chunker   chunker.Chunker
}

func NewIndexer(db *sql.DB, tok tokenizer.Tokenizer) *Indexer {
	return &Indexer{
		db:        db,
		tokenizer: tok,
		chunker:   chunker.NewMarkdownChunker(900),
	}
}

func (idx *Indexer) UpdateCollection(collectionName, rootDir, globPattern string, ignorePatterns []string) (*UpdateResult, error) {
	result := &UpdateResult{}

	pattern := globPattern
	if pattern == "" {
		pattern = "**/*.md"
	}

	existingDocs, err := store.ListDocumentsByCollection(idx.db, collectionName)
	if err != nil {
		return nil, err
	}
	existingPaths := make(map[string]string)
	for _, d := range existingDocs {
		existingPaths[d.Path] = d.Hash
	}

	foundPaths := make(map[string]bool)

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			return nil
		}

		matched := false
		if strings.Contains(pattern, "**/") {
			matched, _ = filepath.Match(strings.TrimPrefix(pattern, "**/"), filepath.Base(relPath))
		} else {
			matched, _ = filepath.Match(pattern, filepath.Base(relPath))
		}
		if !matched {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		hash := hashContent(content)
		relPath = filepath.ToSlash(relPath)
		foundPaths[relPath] = true

		if existingHash, exists := existingPaths[relPath]; exists {
			if existingHash == hash {
				result.Unchanged++
				return nil
			}
			result.Updated++
		} else {
			result.Indexed++
		}

		title := extractTitle(string(content), relPath)
		body := string(content)
		tokenizedBody := idx.tokenizer.TokenizeToString(body)
		tokenizedTitle := idx.tokenizer.TokenizeToString(title)

		existingDoc, _ := store.GetDocumentByPath(idx.db, collectionName, relPath)
		if existingDoc != nil {
			store.DeleteVectorsByDocID(idx.db, existingDoc.ID)
		}

		doc := &store.DocumentRecord{
			Collection: collectionName,
			Path:       relPath,
			Title:      title,
			Body:       body,
			Hash:       hash,
			FileSize:   int64(len(content)),
		}

		if err := store.UpsertDocument(idx.db, doc, tokenizedBody, tokenizedTitle); err != nil {
			return err
		}

		return idx.createChunks(doc.ID, title, body, hash)
	})
	if err != nil {
		return nil, err
	}

	for path := range existingPaths {
		if !foundPaths[path] {
			doc, err := store.GetDocumentByPath(idx.db, collectionName, path)
			if err == nil {
				store.DeleteVectorsByDocID(idx.db, doc.ID)
				store.DeleteDocument(idx.db, doc.ID)
				result.Removed++
			}
		}
	}

	return result, nil
}

func (idx *Indexer) createChunks(docID int64, title, body, hash string) error {
	chunks, err := idx.chunker.Chunk(title, body)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}

	data := make([]store.ChunkData, len(chunks))
	for i, c := range chunks {
		data[i] = store.ChunkData{
			Content:    c.Content,
			Position:   c.Position,
			TokenCount: c.TokenCount,
			Hash:       hash,
		}
	}
	_, err = store.InsertChunks(idx.db, docID, data)
	return err
}

func hashContent(content []byte) string {
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}

var headingRe = regexp.MustCompile(`^#\s+(.+)$`)

func extractTitle(content, fallback string) string {
	lines := strings.SplitN(content, "\n", 20)
	for _, line := range lines {
		m := headingRe.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return strings.TrimSuffix(filepath.Base(fallback), filepath.Ext(fallback))
}
