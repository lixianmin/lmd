package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lixianmin/lmd/internal/chunker"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

const (
	defaultChunkSize  = 300 // 每个分块的目标 rune 数
	titleScanMaxLines = 20  // 提取标题时最多扫描的行数
)

type DocAction int

const (
	DocNew DocAction = iota
	DocChanged
	DocDeleted
)

type PendingDoc struct {
	Action      DocAction
	Collection  string
	RootDir     string
	Path        string
	Title       string
	Body        string
	Hash        string
	FileSize    int64
	FileModTime int64
	OldDocId    int64
	Chunks      []dao.ChunkData
}

type UpdateResult struct {
	Indexed     int
	Updated     int
	Unchanged   int
	Removed     int
	DirtyDocIds []int64
}

type ChunkIndexer struct {
	tokenizer       tokenizer.Tokenizer
	markdownChunker chunker.Chunker
	plainChunker    chunker.Chunker
	embedProvider   embedding.EmbeddingProvider
}

func NewChunkIndexer(tok tokenizer.Tokenizer, embedProv embedding.EmbeddingProvider) *ChunkIndexer {
	return &ChunkIndexer{
		tokenizer:       tok,
		markdownChunker: chunker.NewMarkdownChunker(defaultChunkSize),
		plainChunker:    chunker.NewPlainTextChunker(defaultChunkSize),
		embedProvider:   embedProv,
	}
}

func (my *ChunkIndexer) chunkerForExt(ext string) chunker.Chunker {
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

func (my *ChunkIndexer) UpdateCollection(collectionName, rootDir, globPattern string, ignorePatterns []string) (*UpdateResult, error) {
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

type existingDocInfo struct {
	id          int64
	hash        string
	fileModTime int64
}

func (my *ChunkIndexer) ScanChanges(collectionName, rootDir, globPattern string, ignorePatterns []string) ([]PendingDoc, error) {
	pattern := globPattern
	if pattern == "" {
		pattern = "**/*.{md,txt}"
	}
	patterns := expandGlobPattern(pattern)

	existingDocs, err := dao.ListDocumentsByCollection(collectionName)
	if err != nil {
		return nil, err
	}
	existingMap := make(map[string]existingDocInfo)
	for _, d := range existingDocs {
		existingMap[d.Path] = existingDocInfo{id: d.Id, hash: d.Hash, fileModTime: d.FileModTime}
	}

	ignoreMatcher := newIgnoreMatcher(ignorePatterns)
	foundPaths := make(map[string]bool)
	var pending []PendingDoc

	err = filepath.WalkDir(rootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			logo.Warn("scanChanges: walk error %s: %s", path, err)
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
			logo.Warn("scanChanges: rel path error %s: %s", path, err)
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
			logo.Warn("scanChanges: stat error %s: %s", path, err)
			return nil
		}
		fileModTime := stat.ModTime().UnixNano()

		relPath = filepath.ToSlash(relPath)
		foundPaths[relPath] = true

		existing, exists := existingMap[relPath]
		if exists {
			if existing.fileModTime == fileModTime {
				return nil
			}

			content, err := os.ReadFile(path)
			if err != nil {
				logo.Warn("scanChanges: read error %s: %s", path, err)
				return nil
			}
			hash := hashContent(content)

			if existing.hash == hash {
				if existing.fileModTime == 0 {
					if err := dao.CompleteDocument(existing.id, hash, fileModTime); err != nil {
						logo.Warn("scanChanges: fixup fileModTime for doc %d: %s", existing.id, err)
					}
				}
				return nil
			}

			title := extractTitle(string(content), relPath)
			body := string(content)
			ch := my.chunkerForExt(filepath.Ext(relPath))
			chunks, _ := ch.Chunk(body)
			chunkData := make([]dao.ChunkData, len(chunks))
			for i, c := range chunks {
				chunkData[i] = dao.ChunkData{
					Content:    c.Content,
					Position:   c.StartLine,
					TokenCount: c.TokenCount,
					Hash:       hash,
				}
			}
			pending = append(pending, PendingDoc{
				Action:      DocChanged,
				Collection:  collectionName,
				RootDir:     rootDir,
				Path:        relPath,
				Title:       title,
				Body:        body,
				Hash:        hash,
				FileSize:    int64(len(content)),
				FileModTime: fileModTime,
				OldDocId:    existing.id,
				Chunks:      chunkData,
			})
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			logo.Warn("scanChanges: read error %s: %s", path, err)
			return nil
		}
		hash := hashContent(content)
		title := extractTitle(string(content), relPath)
		body := string(content)
		ch := my.chunkerForExt(filepath.Ext(relPath))
		chunks, _ := ch.Chunk(body)
		chunkData := make([]dao.ChunkData, len(chunks))
		for i, c := range chunks {
			chunkData[i] = dao.ChunkData{
				Content:    c.Content,
				Position:   c.StartLine,
				TokenCount: c.TokenCount,
				Hash:       hash,
			}
		}
		pending = append(pending, PendingDoc{
			Action:      DocNew,
			Collection:  collectionName,
			RootDir:     rootDir,
			Path:        relPath,
			Title:       title,
			Body:        body,
			Hash:        hash,
			FileSize:    int64(len(content)),
			FileModTime: fileModTime,
			Chunks:      chunkData,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	for path, info := range existingMap {
		if !foundPaths[path] {
			pending = append(pending, PendingDoc{
				Action:     DocDeleted,
				Collection: collectionName,
				RootDir:    rootDir,
				Path:       path,
				OldDocId:   info.id,
			})
		}
	}

	return pending, nil
}

func (my *ChunkIndexer) createChunks(docId int64, body, hash string, ch chunker.Chunker) error {
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

func (my *ChunkIndexer) ScanIncomplete(limit int) ([]PendingDoc, error) {
	docs, err := dao.FindDocsWithMissingEmbeddings(limit)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, nil
	}

	cols, err := dao.ListCollections()
	if err != nil {
		return nil, err
	}
	colMap := make(map[string]dao.CollectionRecord, len(cols))
	for _, c := range cols {
		colMap[c.Name] = c
	}

	var pending []PendingDoc
	for _, doc := range docs {
		col, ok := colMap[doc.Collection]
		if !ok {
			continue
		}

		absPath := filepath.Join(col.Path, doc.Path)
		content, err := os.ReadFile(absPath)
		if err != nil {
			logo.Warn("scanIncomplete: read %s: %s", absPath, err)
			continue
		}

		ch := my.chunkerForExt(filepath.Ext(doc.Path))
		chunks, _ := ch.Chunk(string(content))
		chunkData := make([]dao.ChunkData, len(chunks))
		for i, c := range chunks {
			chunkData[i] = dao.ChunkData{
				Content:    c.Content,
				Position:   c.StartLine,
				TokenCount: c.TokenCount,
				Hash:       doc.Hash,
			}
		}

		pending = append(pending, PendingDoc{
			Action:      DocChanged,
			Collection:  doc.Collection,
			RootDir:     col.Path,
			Path:        doc.Path,
			Title:       doc.Title,
			Body:        string(content),
			Hash:        doc.Hash,
			FileSize:    doc.FileSize,
			FileModTime: doc.FileModTime,
			OldDocId:    doc.Id,
			Chunks:      chunkData,
		})
	}
	return pending, nil
}

func (my *ChunkIndexer) ProcessDoc(ctx context.Context, doc PendingDoc) error {
	if doc.Action != DocDeleted && !dao.CollectionExists(doc.Collection) {
		logo.Info("chunkIndexer: skip doc, collection %q removed", doc.Collection)
		return nil
	}

	switch doc.Action {
	case DocDeleted:
		logo.Info("chunkIndexer: deleting doc %d (%s/%s)", doc.OldDocId, doc.Collection, doc.Path)
		return dao.DeleteDocumentAndSummary(doc.OldDocId)
	case DocChanged:
		if err := dao.DeleteDocumentAndSummary(doc.OldDocId); err != nil {
			return fmt.Errorf("delete old doc: %w", err)
		}
		return my.processDocNew(ctx, doc)
	case DocNew:
		return my.processDocNew(ctx, doc)
	}
	return nil
}

func (my *ChunkIndexer) processDocNew(ctx context.Context, doc PendingDoc) error {
	totalStart := time.Now()

	docId, err := dao.InsertDocument(doc.Collection, doc.Path, doc.Title, doc.Body, doc.FileSize, doc.Hash)
	if err != nil {
		return fmt.Errorf("insert document: %w", err)
	}

	var embedDuration time.Duration
	var insertDuration time.Duration
	batchSize := 8
	for i := 0; i < len(doc.Chunks); i += batchSize {
		end := min(i+batchSize, len(doc.Chunks))
		batch := doc.Chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
		}

		t := time.Now()
		vecs, err := my.embedProvider.EmbedBatch(ctx, texts)
		embedDuration += time.Since(t)
		if err != nil {
			return fmt.Errorf("embed chunks batch %d: %w", i/batchSize, err)
		}

		tokenized := make([]string, len(batch))
		for j, c := range batch {
			tokenized[j] = my.tokenizer.TokenizeToString(c.Content)
		}

		t = time.Now()
		if _, err := dao.InsertChunksAndVectors(docId, doc.Collection, i, batch, tokenized, vecs); err != nil {
			return fmt.Errorf("insert chunks batch %d: %w", i/batchSize, err)
		}
		insertDuration += time.Since(t)
	}

	if err := dao.CompleteDocument(docId, doc.Hash, doc.FileModTime); err != nil {
		return fmt.Errorf("complete document: %w", err)
	}

	fullPath := doc.RootDir + "/" + doc.Path
	total := time.Since(totalStart)
	logo.Info("chunkIndexer: doc %d (%s) chunks=%d embed=%.2fs insert=%.2fs total=%.2fs",
		docId, fullPath,
		len(doc.Chunks), embedDuration.Seconds(), insertDuration.Seconds(),
		total.Seconds())

	return nil
}
