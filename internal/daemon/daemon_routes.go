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

func (my *Daemon) handleHybrid(w http.ResponseWriter, r *http.Request) {
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

	logo.Info("handleHybrid: query=%q collection=%s lex=%d vec=%d results=%d",
		req.Query, req.Collection, len(lexHits), len(vecHits), len(results))
	writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results})
}

func (my *Daemon) handleHyde(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
		Strategy   string  `json:"strategy"`
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

	lexHits, lexErr := my.searcher.SearchLex(req.Query, "@hyde", overfetch, 0, req.Strategy)
	vecHits, vecErr := my.searcher.SearchVector(ctx, my.embedProvider, req.Query, "@hyde", overfetch, 0)
	if lexErr != nil && vecErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "hyde search failed"})
		return
	}
	hydeHits := service.FuseResults(lexHits, vecHits)

	if len(hydeHits) == 0 {
		results := my.fullHybridSearch(ctx, req.Query, req.Collection, req.Limit, req.MinScore, req.Strategy)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	uniqueRowIds := make(map[int64]bool)
	for _, h := range hydeHits {
		if h.DocRowId > 0 {
			uniqueRowIds[h.DocRowId] = true
		}
	}
	docIdSet := make(map[int64]bool)
	if len(uniqueRowIds) > 0 {
		ids := make([]int64, 0, len(uniqueRowIds))
		for id := range uniqueRowIds {
			ids = append(ids, id)
		}
		docs, err := dao.GetDocumentsByIds(ids)
		if err == nil {
			for _, doc := range docs {
				if doc.SourceDocId > 0 {
					docIdSet[doc.SourceDocId] = true
				}
			}
		}
	}

	if len(docIdSet) == 0 {
		results := my.fullHybridSearch(ctx, req.Query, req.Collection, req.Limit, req.MinScore, req.Strategy)
		writeJSON(w, http.StatusOK, map[string]interface{}{"hits": results, "routed": false})
		return
	}

	sourceDocIds := make([]int64, 0, len(docIdSet))
	for id := range docIdSet {
		sourceDocIds = append(sourceDocIds, id)
	}

	lexHits2, _ := my.searcher.SearchLexByDocIds(req.Query, sourceDocIds, overfetch*2, req.Strategy)
	vecHits2, _ := my.searcher.SearchVectorByDocIds(ctx, my.embedProvider, req.Query, sourceDocIds, overfetch*2)

	results := service.FuseResults(lexHits2, vecHits2)
	results = filterAndLimit(results, req.MinScore, req.Limit)

	logo.Info("handleHyde: query=%q hyde=%d docs=%d results=%d routed=true",
		req.Query, len(hydeHits), len(docIdSet), len(results))
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
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use collection/path or #doc_id format"})
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
	collections := make([]map[string]any, len(cols))
	for i, c := range cols {
		collections[i] = map[string]any{
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
	pending := max(chunkCount-embedCount, 0)

	hydeTotal, hydeDone := dao.GetHydeCounts()

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
		"database":    my.cfg.Database.Path,
		"documents":   totalDocs,
		"chunks":      chunkCount,
		"embedded":    embedCount,
		"pending":     pending,
		"eta":         eta,
		"hyde_total":  hydeTotal,
		"hyde_done":   hydeDone,
		"collections": collections,
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

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"name":   req.Name,
		"path":   absPath,
		"mask":   mask,
		"status": "added",
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

	err = dao.RemoveCollection(req.Name)
	if err != nil {
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
