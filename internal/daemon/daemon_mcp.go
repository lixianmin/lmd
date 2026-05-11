package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/service"
	"github.com/lixianmin/logo"
)

func (my *Daemon) buildStatus() (interface{}, error) {
	cols, err := dao.ListCollections()
	if err != nil {
		return nil, err
	}
	chunkCounts := dao.GetChunkCountsByCollection()

	totalDocs := 0
	type colStat struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		Glob       string `json:"glob"`
		DocCount   int    `json:"doc_count"`
		ChunkCount int    `json:"chunk_count"`
	}
	stats := make([]colStat, len(cols))
	for i, c := range cols {
		stats[i] = colStat{Name: c.Name, Path: c.Path, Glob: c.GlobPattern, DocCount: c.DocCount, ChunkCount: chunkCounts[c.Name]}
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

	return map[string]interface{}{
		"documents":      totalDocs,
		"chunks":         chunkCount,
		"embedded":       embedCount,
		"pending":        pending,
		"eta":            eta,
		"summary_total":  totalDocsForSummary,
		"summary_done":   summaryCount,
		"collections":    stats,
	}, nil
}

func (my *Daemon) handleToolCall(toolName string, params json.RawMessage) (interface{}, error) {
	switch toolName {
	case "search":
		return my.handleToolSearch(params)
	case "query":
		return my.handleToolQuery(params)
	case "vsearch":
		return my.handleToolVsearch(params)
	case "get":
		return my.handleToolGet(params)
	case "status":
		return my.buildStatus()
	case "list_collections":
		return my.handleToolListCollections(params)
	case "smart_query":
		return my.handleToolSmartQuery(params)
	default:
		return nil, fmt.Errorf("unknown tool: %s", toolName)
	}
}

func (my *Daemon) handleToolSearch(params json.RawMessage) (interface{}, error) {
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
	hits, err := my.searcher.SearchLex(req.Query, req.Collection, req.Limit, req.MinScore, "")
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"hits": hits}, nil
}

func (my *Daemon) handleToolQuery(params json.RawMessage) (interface{}, error) {
	var req struct {
		Query      string  `json:"query"`
		Collection string  `json:"collection"`
		Limit      int     `json:"limit"`
		MinScore   float64 `json:"min_score"`
		Strategy   string  `json:"strategy"`
	}
	if err := convert.FromJsonE(params, &req); err != nil {
		return nil, err
	}
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}
	lexHits, err := my.searcher.SearchLex(req.Query, req.Collection, safeOverfetch(req.Limit), 0, req.Strategy)
	if err != nil {
		return nil, err
	}
	vecHits, err := my.searcher.SearchVector(context.Background(), my.embedProvider, req.Query, req.Collection, safeOverfetch(req.Limit), 0)
	if err != nil {
		return nil, err
	}
	results := service.FuseResults(lexHits, vecHits)
	logo.Info("handleToolCall: query query=%q lex=%d vec=%d results=%d",
		req.Query, len(lexHits), len(vecHits), len(results))
	results = filterAndLimit(results, req.MinScore, req.Limit)
	return map[string]interface{}{"hits": results}, nil
}

func (my *Daemon) handleToolVsearch(params json.RawMessage) (interface{}, error) {
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
	hits, err := my.searcher.SearchVector(context.Background(), my.embedProvider, req.Query, req.Collection, req.Limit, req.MinScore)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"hits": hits}, nil
}

func (my *Daemon) handleToolGet(params json.RawMessage) (interface{}, error) {
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
}

func (my *Daemon) handleToolListCollections(params json.RawMessage) (interface{}, error) {
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
}

func (my *Daemon) handleToolSmartQuery(params json.RawMessage) (interface{}, error) {
	var req struct {
		Query    string  `json:"query"`
		Limit    int     `json:"limit"`
		MinScore float64 `json:"min_score"`
		Strategy string  `json:"strategy"`
	}
	if err := convert.FromJsonE(params, &req); err != nil {
		return nil, err
	}
	if req.Limit <= 0 {
		req.Limit = defaultSearchLimit
	}
	if req.Strategy == "" {
		req.Strategy = "pos-or"
	}

	overfetch := safeOverfetch(req.Limit)

	lexHits, _ := my.searcher.SearchLex(req.Query, "@summaries", overfetch, 0, req.Strategy)
	vecHits, _ := my.searcher.SearchVector(context.Background(), my.embedProvider, req.Query, "@summaries", overfetch, 0)
	summaryHits := service.FuseResults(lexHits, vecHits)

	if len(summaryHits) == 0 {
		results := my.fullHybridSearch(context.Background(), req.Query, "", req.Limit, req.MinScore, req.Strategy)
		return map[string]interface{}{"hits": results, "routed": false}, nil
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
			logo.Warn("handleToolSmartQuery: resolve doc ids failed: %s", err)
		}
		for _, doc := range docs {
			if doc.SourceDocId > 0 {
				docIdSet[doc.SourceDocId] = true
			}
		}
	}

	if len(docIdSet) == 0 {
		results := my.fullHybridSearch(context.Background(), req.Query, "", req.Limit, req.MinScore, req.Strategy)
		return map[string]interface{}{"hits": results, "routed": false}, nil
	}

	sourceDocIds := make([]int64, 0, len(docIdSet))
	for id := range docIdSet {
		sourceDocIds = append(sourceDocIds, id)
	}

	lexHits2, _ := my.searcher.SearchLexByDocIds(req.Query, sourceDocIds, overfetch*2, req.Strategy)
	vecHits2, _ := my.searcher.SearchVectorByDocIds(context.Background(), my.embedProvider, req.Query, sourceDocIds, overfetch*2)

	results := service.FuseResults(lexHits2, vecHits2)
	results = filterAndLimit(results, req.MinScore, req.Limit)

	logo.Info("handleToolSmartQuery: query=%q summary=%d docs=%d results=%d routed=true",
		req.Query, len(summaryHits), len(docIdSet), len(results))
	return map[string]interface{}{"hits": results, "routed": true}, nil
}

func truncateForLog(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
