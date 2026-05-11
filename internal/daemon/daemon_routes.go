package daemon

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/mcp"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/logo"
)

const (
	defaultSearchLimit    = 5    // 搜索默认返回条数
	defaultVectorMinScore = 0.3  // 向量搜索最低分数阈值
	overfetchMultiplier   = 3    // 混合搜索 pre-fusion 超取倍数
	docPreviewMaxRunes    = 500  // 文档预览最大 rune 数
	maxOverfetchLimit     = 1000 // overfetch 上限，防止 int 溢出
)

func safeOverfetch(limit int) int {
	fetch := limit * overfetchMultiplier
	if fetch <= 0 || fetch > maxOverfetchLimit {
		return maxOverfetchLimit
	}
	return fetch
}

func filterAndLimit(hits []formatter.SearchHit, minScore float64, limit int) []formatter.SearchHit {
	if minScore > 0 {
		var filtered []formatter.SearchHit
		for _, h := range hits {
			if h.Score >= minScore {
				filtered = append(filtered, h)
			}
		}
		hits = filtered
	}
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func (my *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (my *Daemon) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
		Format     string  `json:"format"`
		JSON       bool    `json:"json"`
		Strategy   string  `json:"strategy"` // FTS 查询策略: "pos-or"(默认) / "or" / "df" / "pos-must" / "pos-weight"
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}

	results, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit, req.MinScore, req.Strategy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	logo.Info("handleSearch: query=%q collection=%s results=%d", req.Query, req.Collection, len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results})
}

func (my *Daemon) handleVsearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}
	if req.MinScore == 0 {
		req.MinScore = defaultVectorMinScore
	}

	results, err := my.searcher.SearchVector(r.Context(), my.embedProvider, req.Query, req.Collection, req.Limit, req.MinScore)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	logo.Info("handleVsearch: query=%q collection=%s results=%d", req.Query, req.Collection, len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results})
}

func (my *Daemon) handleQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
		Strategy   string  `json:"strategy"` // FTS 查询策略: "pos-or"(默认) / "or" / "df" / "pos-must" / "pos-weight"
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}

	lexHits, err := my.searcher.SearchLex(req.Query, req.Collection, safeOverfetch(req.Limit), 0, req.Strategy)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	vecHits, err := my.searcher.SearchVector(r.Context(), my.embedProvider, req.Query, req.Collection, safeOverfetch(req.Limit), 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	results := service.FuseResults(lexHits, vecHits)
	results = filterAndLimit(results, req.MinScore, req.Limit)

	logo.Info("handleQuery: query=%q collection=%s lex=%d vec=%d results=%d",
		req.Query, req.Collection, len(lexHits), len(vecHits), len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results})
}

func (my *Daemon) handleHyde(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"error": "HyDE not available",
		"hits":  []formatter.SearchHit{},
	})
}

func (my *Daemon) handleSmartQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query    string  `json:"query"`
		Limit    int     `json:"limit"`
		MinScore float64 `json:"min_score"`
		Strategy string  `json:"strategy"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}
	if req.Strategy == "" {
		req.Strategy = "pos-or"
	}

	ctx := r.Context()
	overfetch := safeOverfetch(req.Limit)

	lexHits, lexErr := my.searcher.SearchLex(req.Query, "@summaries", overfetch, 0, req.Strategy)
	vecHits, vecErr := my.searcher.SearchVector(ctx, my.embedProvider, req.Query, "@summaries", overfetch, 0)
	if lexErr != nil {
		logo.Warn("handleSmartQuery: lex search summaries failed: %s", lexErr)
	}
	if vecErr != nil {
		logo.Warn("handleSmartQuery: vector search summaries failed: %s", vecErr)
	}
	summaryHits := service.FuseResults(lexHits, vecHits)
	logo.Info("handleSmartQuery: level1 query=%q lexHits=%d vecHits=%d summaryHits=%d", req.Query, len(lexHits), len(vecHits), len(summaryHits))

	if len(summaryHits) == 0 {
		results := my.fullHybridSearch(ctx, req.Query, "", req.Limit, req.MinScore, req.Strategy)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	for i, h := range summaryHits {
		snippet := h.Snippet
		if len([]rune(snippet)) > 200 {
			snippet = string([]rune(snippet)[:200]) + "..."
		}
		logo.Info("handleSmartQuery: level1[%d] score=%.4f chunkId=%d docRowId=%d title=%q snippet=%q", i, h.Score, h.ChunkId, h.DocRowId, h.Title, snippet)
	}

	docIdSet := make(map[int64]bool)
	uniqueRowIds := make(map[int64]bool)
	for _, h := range summaryHits {
		if h.DocRowId > 0 {
			uniqueRowIds[h.DocRowId] = true
		}
	}
	if len(uniqueRowIds) > 0 {
		ids := make([]int64, 0, len(uniqueRowIds))
		for id := range uniqueRowIds {
			ids = append(ids, id)
		}
		docs, err := dao.GetDocumentsByIds(ids)
		if err != nil {
			logo.Warn("handleSmartQuery: resolve doc ids failed: %s", err)
		}
		for _, doc := range docs {
			logo.Info("handleSmartQuery: summary docId=%d path=%q sourceDocId=%d", doc.Id, doc.Path, doc.SourceDocId)
			if doc.SourceDocId > 0 {
				docIdSet[doc.SourceDocId] = true
			}
		}
	}

	if len(docIdSet) == 0 {
		results := my.fullHybridSearch(ctx, req.Query, "", req.Limit, req.MinScore, req.Strategy)
		writeJSON(w, http.StatusOK, map[string]any{"hits": results, "routed": false})
		return
	}

	sourceDocIds := make([]int64, 0, len(docIdSet))
	for id := range docIdSet {
		sourceDocIds = append(sourceDocIds, id)
	}
	logo.Info("handleSmartQuery: level2 query=%q sourceDocIds=%v", req.Query, sourceDocIds)

	lexHits2, lexErr2 := my.searcher.SearchLexByDocIds(req.Query, sourceDocIds, overfetch*2, req.Strategy)
	vecHits2, vecErr2 := my.searcher.SearchVectorByDocIds(ctx, my.embedProvider, req.Query, sourceDocIds, overfetch*2)
	if lexErr2 != nil {
		logo.Warn("handleSmartQuery: level2 lex search failed: %s", lexErr2)
	}
	if vecErr2 != nil {
		logo.Warn("handleSmartQuery: level2 vector search failed: %s", vecErr2)
	}

	results := service.FuseResults(lexHits2, vecHits2)
	results = filterAndLimit(results, req.MinScore, req.Limit)

	logo.Info("handleSmartQuery: query=%q summary=%d docs=%d results=%d routed=true",
		req.Query, len(summaryHits), len(docIdSet), len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": true})
}

func (my *Daemon) fullHybridSearch(ctx context.Context, query, collection string, limit int, minScore float64, strategy string) []formatter.SearchHit {
	overfetch := safeOverfetch(limit)
	lexHits, _ := my.searcher.SearchLex(query, collection, overfetch, 0, strategy)
	vecHits, _ := my.searcher.SearchVector(ctx, my.embedProvider, query, collection, overfetch, 0)
	results := service.FuseResults(lexHits, vecHits)
	return filterAndLimit(results, minScore, limit)
}

func (my *Daemon) handleGet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Full  bool   `json:"full"`
		From  int    `json:"from"`
		Lines int    `json:"lines"`
	}
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(bodyBytes, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	input := req.Path
	var doc *dao.DocumentRecord

	if strings.HasPrefix(input, "#") {
		doc, err = dao.GetDocumentByDocId(input[1:])
	} else {
		parts := strings.SplitN(input, "/", 2)
		if len(parts) == 2 {
			doc, err = dao.GetDocumentByPath(parts[0], parts[1])
		} else {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use collection/path or #docid format"})
			return
		}
	}

	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	body := doc.Body
	if req.From > 0 || req.Lines > 0 {
		lines := strings.Split(body, "\n")
		start := 0
		end := len(lines)
		if req.From > 0 && req.From <= len(lines) {
			start = req.From - 1
		}
		if req.Lines > 0 && req.Lines < end-start {
			end = start + req.Lines
		}
		if start < end {
			body = strings.Join(lines[start:end], "\n")
		} else if start < len(lines) {
			body = strings.Join(lines[start:], "\n")
		}
	}
	if !req.Full {
		runes := []rune(body)
		if len(runes) > docPreviewMaxRunes {
			body = string(runes[:docPreviewMaxRunes]) + "..."
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"doc_id":     dao.ShortDocId(doc.DocId),
		"title":      doc.Title,
		"collection": doc.Collection,
		"path":       doc.Path,
		"file_size":  doc.FileSize,
		"body":       body,
	})
}

func (my *Daemon) handleStatus(w http.ResponseWriter, r *http.Request) {
	cols, err := dao.ListCollections()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	chunkCounts := dao.GetChunkCountsByCollection()

	totalDocs := 0
	collections := make([]map[string]interface{}, len(cols))
	for i, c := range cols {
		collections[i] = map[string]interface{}{
			"name":        c.Name,
			"path":        c.Path,
			"glob":        c.GlobPattern,
			"doc_count":   c.DocCount,
			"chunk_count": chunkCounts[c.Name],
			"ignore":      c.IgnorePatterns,
			"created_at":  c.CreatedAt,
		}
		totalDocs += c.DocCount
	}

	chunkCount, embedCount := dao.GetChunkCounts()
	pending := chunkCount - embedCount
	if pending < 0 {
		pending = 0
	}

	totalDocsForSummary, summaryCount := dao.GetSummaryCounts()

	var eta string
	if pending > 0 {
		startNum := my.etaStartNum.Load()
		startAt := my.etaStartAt.Load()
		if startAt > 0 {
			delta := int64(embedCount) - startNum
			elapsed := time.Since(time.Unix(0, startAt)).Seconds()
			if elapsed > 0 && delta > 0 {
				speed := float64(delta) / elapsed
				eta = formatETA(time.Duration(float64(pending)/speed) * time.Second)
			}
		}
	}
	if eta == "" {
		eta = "calculating..."
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"database":       my.cfg.Database.Path,
		"documents":      totalDocs,
		"chunks":         chunkCount,
		"embedded":       embedCount,
		"pending":        pending,
		"eta":            eta,
		"summary_total":  totalDocsForSummary,
		"summary_done":   summaryCount,
		"collections":    collections,
	})
}

func formatETA(d time.Duration) string {
	if d <= 0 {
		return "<1s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	// 超过 1 小时只显示 h 和 m
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m > 0 {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	return fmt.Sprintf("%dh", h)
}

func (my *Daemon) handleCollectionAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
		Mask string `json:"mask"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if strings.HasPrefix(req.Name, "@") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "collection name cannot start with '@'"})
		return
	}

	absPath := req.Path
	if !filepath.IsAbs(absPath) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path must be absolute"})
		return
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "path does not exist"})
		return
	}

	mask := req.Mask
	if mask == "" {
		mask = "**/*.{md,txt}"
	}

	if err := dao.AddCollection(req.Name, absPath, mask, nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var indexed int
	if my.indexer != nil {
		my.rebuildMu.RLock()
		result, err := my.indexer.UpdateCollection(req.Name, absPath, mask, nil)
		my.rebuildMu.RUnlock()
		if err == nil {
			indexed = result.Indexed + result.Updated
			logo.Info("handleCollectionAdd: indexed %s +%d ~%d", req.Name, result.Indexed, result.Updated)
		}
	}

	logo.Info("handleCollectionAdd: name=%s path=%s mask=%s indexed=%d", req.Name, absPath, mask, indexed)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"name":    req.Name,
		"path":    absPath,
		"mask":    mask,
		"indexed": indexed,
	})
}

func (my *Daemon) handleCollectionRemove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if strings.HasPrefix(req.Name, "@") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot remove system collection"})
		return
	}

	if err := dao.RemoveCollection(req.Name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	logo.Info("handleCollectionRemove: name=%s", req.Name)
	writeJSON(w, http.StatusOK, map[string]string{"name": req.Name, "status": "removed"})
}

func (my *Daemon) handleCollectionList(w http.ResponseWriter, r *http.Request) {
	cols, err := dao.ListCollections()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	type colInfo struct {
		Name     string   `json:"name"`
		Path     string   `json:"path"`
		Glob     string   `json:"glob"`
		DocCount int      `json:"doc_count"`
		Ignore   []string `json:"ignore,omitempty"`
	}

	result := make([]colInfo, len(cols))
	for i, c := range cols {
		result[i] = colInfo{
			Name:     c.Name,
			Path:     c.Path,
			Glob:     c.GlobPattern,
			DocCount: c.DocCount,
			Ignore:   c.IgnorePatterns,
		}
	}

	writeJSON(w, http.StatusOK, result)
}

func (my *Daemon) handleCollectionRename(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := convert.FromJsonE(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if strings.HasPrefix(req.New, "@") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "collection name cannot start with '@'"})
		return
	}

	if err := dao.RenameCollection(req.Old, req.New); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	logo.Info("handleCollectionRename: %s -> %s", req.Old, req.New)
	writeJSON(w, http.StatusOK, map[string]string{"old": req.Old, "new": req.New, "status": "renamed"})
}

func (my *Daemon) handleRebuild(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	logo.Info("handleRebuild: starting")

	my.rebuildMu.Lock()
	cols, err := dao.ListCollections()
	if err != nil {
		my.rebuildMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(cols) == 0 {
		my.rebuildMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"status": "no collections"})
		return
	}

	dao.CloseFTSStatements()
	store := dao.DB
	dao.DB = nil
	store.Close()

	dbPath := my.cfg.Database.Path
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		my.rebuildMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := dao.Init(dbPath); err != nil {
		my.rebuildMu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for _, col := range cols {
		if err := dao.AddCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns); err != nil {
			logo.Error("handleRebuild: restore collection %s failed: %s", col.Name, err)
		}
	}
	my.rebuildMu.Unlock()

	my.syncIndex()

	logo.Info("handleRebuild: collections restored, background embedTicker will handle embedding")
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"collections": len(cols),
		"elapsed":     time.Since(start).String(),
	})
}

func (my *Daemon) handleMCP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	req, err := mcp.ParseLine(body)
	if err != nil || req == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON-RPC request"})
		return
	}

	resp := mcp.HandleRequest(*req)
	writeJSON(w, http.StatusOK, resp)
}
