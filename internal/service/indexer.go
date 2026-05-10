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

const (
	defaultChunkSize  = 300 // 每个分块的目标 rune 数
	titleScanMaxLines = 20  // 提取标题时最多扫描的行数
)

type UpdateResult struct {
	Indexed     int
	Updated     int
	Unchanged   int
	Removed     int
	DirtyDocIds []int64
}

type Indexer struct {
	tokenizer       tokenizer.Tokenizer
	markdownChunker chunker.Chunker
	plainChunker    chunker.Chunker
}

func NewIndexer(tok tokenizer.Tokenizer) *Indexer {
	return &Indexer{
		tokenizer:       tok,
		markdownChunker: chunker.NewMarkdownChunker(defaultChunkSize),
		plainChunker:    chunker.NewPlainTextChunker(defaultChunkSize),
	}
}

func (my *Indexer) chunkerForExt(ext string) chunker.Chunker {
	switch ext {
	case ".txt":
		return my.plainChunker
	default:
		return my.markdownChunker
	}
}

func expandGlobPattern(pattern string) []string {
	start := strings.Index(pattern, "{")
	end := strings.Index(pattern, "}")
	if start < 0 || end < 0 || end <= start {
		return []string{pattern}
	}

	prefix := pattern[:start]
	suffix := pattern[end+1:]
	alternatives := strings.Split(pattern[start+1:end], ",")

	var result []string
	for _, alt := range alternatives {
		result = append(result, prefix+strings.TrimSpace(alt)+suffix)
	}
	return result
}

type docInfo struct {
	hash        string
	fileModTime int64
}

func (my *Indexer) UpdateCollection(collectionName, rootDir, globPattern string, ignorePatterns []string) (*UpdateResult, error) {
	result := &UpdateResult{}

	pattern := globPattern
	if pattern == "" {
		pattern = "**/*.{md,txt}"
	}

	patterns := expandGlobPattern(pattern)

	existingDocs, err := dao.ListDocumentsByCollection(collectionName)
	if err != nil {
		return nil, err
	}
	existingPaths := make(map[string]docInfo)
	for _, d := range existingDocs {
		existingPaths[d.Path] = docInfo{hash: d.Hash, fileModTime: d.FileModTime}
	}

	ignoreMatcher := newIgnoreMatcher(ignorePatterns)

	foundPaths := make(map[string]bool)

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			logo.Warn("indexer: walk error %s: %s", path, err)
			return nil
		}
		if d.IsDir() {
			if ignoreMatcher.matchDir(path) {
				return fs.SkipDir
			}
			return nil
		}

		if ignoreMatcher.matchFile(path) {
			return nil
		}

		relPath, err := filepath.Rel(rootDir, path)
		if err != nil {
			logo.Warn("indexer: rel path error %s: %s", path, err)
			return nil
		}

		matched := false
		for _, p := range patterns {
			if strings.Contains(p, "**/") {
				matched, _ = filepath.Match(strings.TrimPrefix(p, "**/"), filepath.Base(relPath))
			} else {
				matched, _ = filepath.Match(p, filepath.Base(relPath))
			}
			if matched {
				break
			}
		}
		if !matched {
			return nil
		}

		stat, err := os.Stat(path)
		if err != nil {
			logo.Warn("indexer: stat error %s: %s", path, err)
			return nil
		}
		fileModTime := stat.ModTime().UnixNano()

		relPath = filepath.ToSlash(relPath)
		foundPaths[relPath] = true

		if existing, exists := existingPaths[relPath]; exists {
			if existing.fileModTime == fileModTime {
				result.Unchanged++
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				logo.Warn("indexer: read error %s: %s", path, err)
				return nil
			}
			hash := hashContent(content)

			if existing.hash == hash {
				doc := &dao.DocumentRecord{
					Collection:  collectionName,
					Path:        relPath,
					Hash:        hash,
					FileSize:    int64(len(content)),
					FileModTime: fileModTime,
				}
				if err := dao.UpsertDocument(doc); err != nil {
					logo.Warn("indexer: upsert fileModTime for %s/%s failed: %s", collectionName, relPath, err)
				}
				result.Unchanged++
				return nil
			}

			result.Updated++

			title := extractTitle(string(content), relPath)
			body := string(content)

			existingDoc, _ := dao.GetDocumentByPath(collectionName, relPath)
			if existingDoc != nil {
				logo.Info("indexer: updating %s/%s (old chunks deleted)", collectionName, relPath)
				if err := dao.DeleteVectorsByDocId(existingDoc.Id); err != nil {
					return err
				}
			}

			doc := &dao.DocumentRecord{
				Collection:  collectionName,
				Path:        relPath,
				Title:       title,
				Body:        body,
				Hash:        hash,
				FileSize:    int64(len(content)),
				FileModTime: fileModTime,
			}

			if err := dao.UpsertDocument(doc); err != nil {
				return err
			}

			result.DirtyDocIds = append(result.DirtyDocIds, doc.Id)

			ch := my.chunkerForExt(filepath.Ext(relPath))
			return my.createChunks(doc.Id, body, hash, ch)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			logo.Warn("indexer: read error %s: %s", path, err)
			return nil
		}

		hash := hashContent(content)
		result.Indexed++

		title := extractTitle(string(content), relPath)
		body := string(content)

		doc := &dao.DocumentRecord{
			Collection:  collectionName,
			Path:        relPath,
			Title:       title,
			Body:        body,
			Hash:        hash,
			FileSize:    int64(len(content)),
			FileModTime: fileModTime,
		}

		if err := dao.UpsertDocument(doc); err != nil {
			return err
		}

		result.DirtyDocIds = append(result.DirtyDocIds, doc.Id)

		ch := my.chunkerForExt(filepath.Ext(relPath))
		return my.createChunks(doc.Id, body, hash, ch)
	})
	if err != nil {
		return nil, err
	}

	for path := range existingPaths {
		if !foundPaths[path] {
			doc, err := dao.GetDocumentByPath(collectionName, path)
			if err == nil {
				logo.Info("indexer: removing deleted file %s/%s", collectionName, path)
				dao.DeleteDocument(doc.Id)
				result.Removed++
			}
		}
	}

	return result, nil
}

func (my *Indexer) createChunks(docId int64, body, hash string, ch chunker.Chunker) error {
	chunks, err := ch.Chunk(body)
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
		tokenized[i] = my.tokenizer.TokenizeToString(c.Content)
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
	lines := strings.SplitN(content, "\n", titleScanMaxLines)
	for _, line := range lines {
		m := headingRe.FindStringSubmatch(strings.TrimSpace(line))
		if len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return strings.TrimSuffix(filepath.Base(fallback), filepath.Ext(fallback))
}

type ignoreMatcher struct {
	patterns []string
}

func newIgnoreMatcher(patterns []string) ignoreMatcher {
	return ignoreMatcher{patterns: patterns}
}

func (my ignoreMatcher) matchDir(path string) bool {
	for _, p := range my.patterns {
		if filepath.Base(path) == p {
			return true
		}
	}
	return false
}

func (my ignoreMatcher) matchFile(path string) bool {
	name := filepath.Base(path)
	for _, p := range my.patterns {
		if matched, _ := filepath.Match(p, name); matched {
			return true
		}
		if name == p {
			return true
		}
	}
	return false
}
