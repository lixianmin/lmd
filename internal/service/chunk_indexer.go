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
	DocUpdateEmbeds
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
		fileModTime := stat.ModTime().Unix()

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
					if err := dao.UpdateFileModTime(existing.id, fileModTime); err != nil {
						logo.Warn("scanChanges: fixup fileModTime for doc %d: %s", existing.id, err)
					}
				} else {
					logo.Info("scanChanges: %s mtime drifted (db=%d file=%d hash ok)", relPath, existing.fileModTime, fileModTime)
				}
				return nil
			}

			logo.Info("scanChanges: %s changed (db=%s file=%s mtime db=%d file=%d)", relPath, trunc16(existing.hash), trunc16(hash), existing.fileModTime, fileModTime)

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

func trunc16(s string) string {
	if len(s) > 16 {
		return s[:16]
	}
	return s
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
			Action:      DocUpdateEmbeds,
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
		return dao.DeleteDocument(doc.OldDocId)
	case DocChanged:
		if err := dao.DeleteDocument(doc.OldDocId); err != nil {
			return fmt.Errorf("delete old doc: %w", err)
		}
		return my.processDocNew(ctx, doc)
	case DocNew:
		return my.processDocNew(ctx, doc)
	case DocUpdateEmbeds:
		return my.processEmbeds(ctx, doc)
	}
	return nil
}

func (my *ChunkIndexer) processEmbeds(ctx context.Context, doc PendingDoc) error {
	chunks, err := dao.GetChunksByDocId(doc.OldDocId)
	if err != nil {
		return fmt.Errorf("get chunks: %w", err)
	}
	if len(chunks) == 0 {
		return nil
	}

	batchSize := 8
	for i := 0; i < len(chunks); i += batchSize {
		end := min(i+batchSize, len(chunks))
		batch := chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.Content
		}

		vecs, err := my.embedProvider.EmbedBatch(ctx, texts)
		if err != nil {
			return fmt.Errorf("embed batch: %w", err)
		}

		items := make([]struct {
			ChunkId    int64
			DocId      int64
			Collection string
			Embedding  []float32
		}, len(batch))
		for j, c := range batch {
			items[j].ChunkId = c.Id
			items[j].DocId = doc.OldDocId
			items[j].Collection = doc.Collection
			items[j].Embedding = vecs[j]
		}

		if err := dao.InsertVectors(items); err != nil {
			return fmt.Errorf("insert vectors: %w", err)
		}
	}
	return nil
}

func (my *ChunkIndexer) processDocNew(ctx context.Context, doc PendingDoc) error {
	totalStart := time.Now()

	docId, err := dao.InsertDocument(doc.Collection, doc.Path, doc.Title, doc.Body, doc.FileSize, doc.FileModTime, doc.Hash)
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

	fullPath := doc.RootDir + "/" + doc.Path
	total := time.Since(totalStart)
	logo.Info("chunkIndexer: doc %d (%s) chunks=%d embed=%.2fs insert=%.2fs total=%.2fs",
		docId, fullPath,
		len(doc.Chunks), embedDuration.Seconds(), insertDuration.Seconds(),
		total.Seconds())

	return nil
}
