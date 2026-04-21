package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/mcp"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/logo"
)

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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 5
	}

	results, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit, req.MinScore)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 5
	}
	if req.MinScore == 0 {
		req.MinScore = 0.3
	}

	results, err := my.searcher.SearchVector(my.provider, req.Query, req.Collection, req.Limit, req.MinScore)
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 5
	}

	lexHits, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit*3, 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	vecHits, err := my.searcher.SearchVectorWithPRF(my.provider, req.Query, req.Collection, req.Limit*3, lexHits)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	results := service.FuseResults(lexHits, vecHits)
	results = my.searcher.ApplyMMR(results, my.provider, req.Query, 0.7, req.Limit)

	if req.MinScore > 0 {
		var filtered []formatter.SearchHit
		for _, h := range results {
			if h.Score >= req.MinScore {
				filtered = append(filtered, h)
			}
		}
		results = filtered
	}

	if req.Limit > 0 && len(results) > req.Limit {
		results = results[:req.Limit]
	}

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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 5
	}

	resp := map[string]any{}

	if my.hydeClient == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"error": "HyDE not available",
			"hits":  []formatter.SearchHit{},
		})
		return
	}

	t0 := time.Now()
	hydeDoc, hydeErr := my.hydeClient.Generate(context.Background(), req.Query)
	hydeDur := time.Since(t0)
	resp["hyde_generate_ms"] = hydeDur.Milliseconds()

	if hydeErr != nil {
		logo.Warn("handleHyde: generate failed: %s (%s)", hydeErr, hydeDur)
		resp["hyde_error"] = hydeErr.Error()
		resp["hits"] = []formatter.SearchHit{}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp["hyde_document"] = hydeDoc
	logo.Info("handleHyde: generated (%s): %s", hydeDur, truncateForLog(hydeDoc, 300))

	hydeVec, embedErr := my.provider.Embed(context.Background(), hydeDoc)
	if embedErr != nil {
		logo.Warn("handleHyde: embed failed: %s", embedErr)
		resp["hyde_embed_error"] = embedErr.Error()
		resp["hits"] = []formatter.SearchHit{}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	fetchLimit := req.Limit * 3
	hydeHits := my.searcher.SearchVectorByEmbedding(hydeVec, req.Collection, fetchLimit)
	resp["hyde_hits"] = len(hydeHits)
	logo.Info("handleHyde: hyde_hits=%d", len(hydeHits))

	results := hydeHits
	if req.MinScore > 0 {
		var filtered []formatter.SearchHit
		for _, h := range results {
			if h.Score >= req.MinScore {
				filtered = append(filtered, h)
			}
		}
		results = filtered
	}
	if req.Limit > 0 && len(results) > req.Limit {
		results = results[:req.Limit]
	}

	resp["hits"] = results
	writeJSON(w, http.StatusOK, resp)
}

func (my *Daemon) handleGet(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path  string `json:"path"`
		Full  bool   `json:"full"`
		From  int    `json:"from"`
		Lines int    `json:"lines"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	input := req.Path
	var doc *dao.DocumentRecord
	var err error

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
	if !req.Full {
		if len(body) > 500 {
			body = body[:500] + "..."
		}
	}
	if req.From > 0 {
		lines := strings.Split(body, "\n")
		if req.From <= len(lines) {
			body = strings.Join(lines[req.From-1:], "\n")
		}
	}
	if req.Lines > 0 {
		lines := strings.Split(body, "\n")
		if req.Lines < len(lines) {
			body = strings.Join(lines[:req.Lines], "\n")
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

	totalDocs := 0
	collections := make([]map[string]interface{}, len(cols))
	for i, c := range cols {
		collections[i] = map[string]interface{}{
			"name":       c.Name,
			"path":       c.Path,
			"glob":       c.GlobPattern,
			"doc_count":  c.DocCount,
			"ignore":     c.IgnorePatterns,
			"created_at": c.CreatedAt,
		}
		totalDocs += c.DocCount
	}

	chunkCount, embedCount := dao.GetChunkCounts()

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"database":    my.cfg.Database.Path,
		"documents":   totalDocs,
		"chunks":      chunkCount,
		"embedded":    embedCount,
		"pending":     chunkCount - embedCount,
		"collections": collections,
	})
}

func (my *Daemon) handleCollectionAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
		Mask string `json:"mask"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
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
		mask = "**/*.md"
	}

	if err := dao.AddCollection(req.Name, absPath, mask, nil); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var indexed int
	if my.indexer != nil {
		result, err := my.indexer.UpdateCollection(req.Name, absPath, mask, nil)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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

	cols, err := dao.ListCollections()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(cols) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no collections"})
		return
	}

	dao.DB.Close()

	dbPath := my.cfg.Database.Path
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if err := dao.Init(dbPath); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for _, col := range cols {
		if err := dao.AddCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns); err != nil {
			logo.Error("handleRebuild: restore collection %s failed: %s", col.Name, err)
		}
	}

	for _, col := range cols {
		result, err := my.indexer.UpdateCollection(col.Name, col.Path, col.GlobPattern, col.IgnorePatterns)
		if err != nil {
			logo.Error("handleRebuild: index %s failed: %s", col.Name, err)
			continue
		}
		logo.Info("handleRebuild: %s indexed=%d updated=%d unchanged=%d removed=%d",
			col.Name, result.Indexed, result.Updated, result.Unchanged, result.Removed)
	}

	embedResult, err := my.embedder.EmbedBatch(context.Background(), 0)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	logo.Info("handleRebuild: done embedded=%d elapsed=%s", embedResult.Embedded, time.Since(start))
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"indexed": embedResult.Embedded,
		"skipped": embedResult.Skipped,
		"elapsed": time.Since(start).String(),
	})
}

func (my *Daemon) handleMemoryAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
		Type    string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content is required"})
		return
	}

	if req.Type == "" {
		req.Type = "episode"
	}

	id, err := my.memSvc.Add(req.Content, req.Type)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	rec, _ := dao.GetMemoryByID(id)
	createdAt := ""
	if rec != nil {
		createdAt = rec.CreatedAt.Format("2006-01-02 15:04:05")
	}

	logo.Info("handleMemoryAdd: id=%d type=%s", id, req.Type)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":         id,
		"type":       req.Type,
		"created_at": createdAt,
	})
}

func (my *Daemon) handleMemorySearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
		Type  string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if req.Limit <= 0 {
		req.Limit = 10
	}

	results, err := my.memSvc.Search(req.Query, req.Limit, req.Type)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	logo.Info("handleMemorySearch: query=%q results=%d", req.Query, len(results))
	writeJSON(w, http.StatusOK, results)
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

func (my *Daemon) buildStatus() (interface{}, error) {
	cols, err := dao.ListCollections()
	if err != nil {
		return nil, err
	}
	totalDocs := 0
	type colStat struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		Glob     string `json:"glob"`
		DocCount int    `json:"doc_count"`
	}
	stats := make([]colStat, len(cols))
	for i, c := range cols {
		stats[i] = colStat{Name: c.Name, Path: c.Path, Glob: c.GlobPattern, DocCount: c.DocCount}
		totalDocs += c.DocCount
	}
	chunkCount, embedCount := dao.GetChunkCounts()
	return map[string]interface{}{
		"documents":   totalDocs,
		"chunks":      chunkCount,
		"embedded":    embedCount,
		"pending":     chunkCount - embedCount,
		"collections": stats,
	}, nil
}

func (my *Daemon) handleToolCall(toolName string, params json.RawMessage) (interface{}, error) {
	switch toolName {
	case "search_lex":
		var req struct {
			Query      string `json:"query"`
			Collection string `json:"collection"`
			Limit      int    `json:"limit"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = 5
		}
		hits, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit, 0)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"hits": hits}, nil

	case "search":
		var req struct {
			Query      string  `json:"query"`
			Collection string  `json:"collection"`
			Limit      int     `json:"limit"`
			MinScore   float64 `json:"min_score"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = 5
		}
		lexHits, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit*3, 0)
		if err != nil {
			return nil, err
		}
	vecHits, err := my.searcher.SearchVectorWithPRF(my.provider, req.Query, req.Collection, req.Limit*3, lexHits)
		if err != nil {
			return nil, err
		}
		results := service.FuseResults(lexHits, vecHits)
		results = my.searcher.ApplyMMR(results, my.provider, req.Query, 0.7, req.Limit)
		logo.Info("handleToolCall: search query=%q lex=%d vec=%d results=%d",
			req.Query, len(lexHits), len(vecHits), len(results))
		if req.MinScore > 0 {
			var filtered []formatter.SearchHit
			for _, h := range results {
				if h.Score >= req.MinScore {
					filtered = append(filtered, h)
				}
			}
			results = filtered
		}
		if req.Limit > 0 && len(results) > req.Limit {
			results = results[:req.Limit]
		}
		return map[string]interface{}{"hits": results}, nil

	case "search_vector":
		var req struct {
			Query      string  `json:"query"`
			Collection string  `json:"collection"`
			Limit      int     `json:"limit"`
			MinScore   float64 `json:"min_score"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = 5
		}
		if req.MinScore == 0 {
			req.MinScore = 0.3
		}
		hits, err := my.searcher.SearchVector(my.provider, req.Query, req.Collection, req.Limit, req.MinScore)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"hits": hits}, nil

	case "get":
		var req struct {
			Path string `json:"path"`
			Full bool   `json:"full"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		parts := strings.SplitN(req.Path, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("use collection/path format")
		}
		doc, err := dao.GetDocumentByPath(parts[0], parts[1])
		if err != nil {
			return nil, err
		}
		body := doc.Body
		if !req.Full && len(body) > 500 {
			body = body[:500] + "..."
		}
		return map[string]interface{}{
			"doc_id": dao.ShortDocId(doc.DocId), "title": doc.Title,
			"collection": doc.Collection, "path": doc.Path, "body": body,
		}, nil

	case "status":
		return my.buildStatus()

	case "list_collections":
		cols, err := dao.ListCollections()
		if err != nil {
			return nil, err
		}
		type colInfo struct {
			Name     string `json:"name"`
			Path     string `json:"path"`
			Glob     string `json:"glob"`
			DocCount int    `json:"doc_count"`
		}
		result := make([]colInfo, len(cols))
		for i, c := range cols {
			result[i] = colInfo{Name: c.Name, Path: c.Path, Glob: c.GlobPattern, DocCount: c.DocCount}
		}
		return result, nil

	case "memory_add":
		var req struct {
			Content string `json:"content"`
			Type    string `json:"type"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Content == "" {
			return nil, fmt.Errorf("content is required")
		}
		if req.Type == "" {
			req.Type = "episode"
		}
		id, err := my.memSvc.Add(req.Content, req.Type)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"id": id, "type": req.Type}, nil

	case "memory_search":
		var req struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
			Type  string `json:"type"`
		}
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = 10
		}
		return my.memSvc.Search(req.Query, req.Limit, req.Type)

	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func truncateForLog(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
