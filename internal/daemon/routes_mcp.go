package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/logo"
)

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
	case "search":
		var req struct {
			Query      string  `json:"query"`
			Collection string  `json:"collection"`
			Limit      int     `json:"limit"`
			MinScore   float64 `json:"min_score"`
		}
		if err := convert.FromJsonE(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = defaultSearchLimit
		}
		hits, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit, req.MinScore)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"hits": hits}, nil

	case "query":
		var req struct {
			Query      string  `json:"query"`
			Collection string  `json:"collection"`
			Limit      int     `json:"limit"`
			MinScore   float64 `json:"min_score"`
		}
		if err := convert.FromJsonE(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = defaultSearchLimit
		}
		lexHits, err := my.searcher.SearchLex(req.Query, req.Collection, safeOverfetch(req.Limit), 0)
		if err != nil {
			return nil, err
		}
		vecHits, err := my.searcher.SearchVector(context.Background(), my.provider, req.Query, req.Collection, safeOverfetch(req.Limit), 0)
		if err != nil {
			return nil, err
		}
		results := service.FuseResults(lexHits, vecHits)
		logo.Info("handleToolCall: query query=%q lex=%d vec=%d results=%d",
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

	case "vsearch":
		var req struct {
			Query      string  `json:"query"`
			Collection string  `json:"collection"`
			Limit      int     `json:"limit"`
			MinScore   float64 `json:"min_score"`
		}
		if err := convert.FromJsonE(params, &req); err != nil {
			return nil, err
		}
		if req.Limit <= 0 {
			req.Limit = defaultSearchLimit
		}
		if req.MinScore == 0 {
			req.MinScore = defaultVectorMinScore
		}
		hits, err := my.searcher.SearchVector(context.Background(), my.provider, req.Query, req.Collection, req.Limit, req.MinScore)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"hits": hits}, nil

	case "get":
		var req struct {
			Path string `json:"path"`
			Full bool   `json:"full"`
		}
		if err := convert.FromJsonE(params, &req); err != nil {
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
		if !req.Full {
			runes := []rune(body)
			if len(runes) > docPreviewMaxRunes {
				body = string(runes[:docPreviewMaxRunes]) + "..."
			}
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
		}
		if err := convert.FromJsonE(params, &req); err != nil {
			return nil, err
		}
		if req.Content == "" {
			return nil, fmt.Errorf("content is required")
		}
		id, err := my.memSvc.Add(req.Content)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"id": id}, nil

	case "memory_delete":
		var req struct {
			ID int64 `json:"id"`
		}
		if err := convert.FromJsonE(params, &req); err != nil {
			return nil, err
		}
		if req.ID <= 0 {
			return nil, fmt.Errorf("id is required")
		}
		if err := dao.DeleteMemory(req.ID); err != nil {
			return nil, fmt.Errorf("memory not found: %d", req.ID)
		}
		return map[string]string{"status": "deleted"}, nil

	case "memory_update":
		var req struct {
			ID      int64  `json:"id"`
			Content string `json:"content"`
		}
		if err := convert.FromJsonE(params, &req); err != nil {
			return nil, err
		}
		if req.ID <= 0 {
			return nil, fmt.Errorf("id is required")
		}
		if req.Content == "" {
			return nil, fmt.Errorf("content is required")
		}
		if err := my.memSvc.Update(req.ID, req.Content); err != nil {
			return nil, err
		}
		return map[string]string{"status": "updated"}, nil

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
