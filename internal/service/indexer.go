package service

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lixianmin/lmd/internal/chunker"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

type UpdateResult struct {
	Indexed   int
	Updated   int
	Unchanged int
	Removed   int
}

type Indexer struct {
	tokenizer tokenizer.Tokenizer
	chunker   chunker.Chunker
}

func NewIndexer(tok tokenizer.Tokenizer) *Indexer {
	return &Indexer{
		tokenizer: tok,
		chunker:   chunker.NewMarkdownChunker(800),
	}
}

func (idx *Indexer) UpdateCollection(collectionName, rootDir, globPattern string, ignorePatterns []string) (*UpdateResult, error) {
	result := &UpdateResult{}

	pattern := globPattern
	if pattern == "" {
		pattern = "**/*.md"
	}

	existingDocs, err := dao.ListDocumentsByCollection(collectionName)
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

		existingDoc, _ := dao.GetDocumentByPath(collectionName, relPath)
		if existingDoc != nil {
			logo.Info("indexer: updating %s/%s (old chunks deleted)", collectionName, relPath)
			dao.DeleteVectorsByDocId(existingDoc.Id)
		}

		doc := &dao.DocumentRecord{
			Collection: collectionName,
			Path:       relPath,
			Title:      title,
			Body:       body,
			Hash:       hash,
			FileSize:   int64(len(content)),
		}

		if err := dao.UpsertDocument(doc); err != nil {
			return err
		}

		return idx.createChunks(doc.Id, body, hash)
	})
	if err != nil {
		return nil, err
	}

	for path := range existingPaths {
		if !foundPaths[path] {
			doc, err := dao.GetDocumentByPath(collectionName, path)
			if err == nil {
				logo.Info("indexer: removing deleted file %s/%s", collectionName, path)
				dao.DeleteVectorsByDocId(doc.Id)
				dao.DeleteDocument(doc.Id)
				result.Removed++
			}
		}
	}

	return result, nil
}

func (idx *Indexer) createChunks(docId int64, body, hash string) error {
	chunks, err := idx.chunker.Chunk(body)
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return nil
	}

	data := make([]dao.ChunkData, len(chunks))
	tokenized := make([]string, len(chunks))
	for i, c := range chunks {
		data[i] = dao.ChunkData{
			Content:    c.Content,
			Position:   c.StartLine,
			TokenCount: c.TokenCount,
			Hash:       hash,
		}
		tokenized[i] = idx.tokenizer.TokenizeToString(c.Content)
	}
	_, err = dao.InsertChunks(docId, data, tokenized)
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
