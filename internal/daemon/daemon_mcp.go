package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lixianmin/got/convert"
	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/llm"
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
		"collections":    stats,
	}, nil
}

func (my *Daemon) handleToolCall(toolName string, params json.RawMessage) (interface{}, error) {
	switch toolName {
	case "search":
		return my.handleToolSearch(params)
	case "hybrid":
		return my.handleToolHybrid(params)
	case "hyde":
		return my.handleToolHyde(params)
	case "vsearch":
		return my.handleToolVsearch(params)
	case "get":
		return my.handleToolGet(params)
	case "status":
		return my.buildStatus()
	case "list_collections":
		return my.handleToolListCollections(params)
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

func (my *Daemon) handleToolHybrid(params json.RawMessage) (interface{}, error) {
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
	logo.Info("handleToolCall: hybrid query=%q lex=%d vec=%d results=%d",
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

func truncateForLog(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func (my *Daemon) handleToolHyde(params json.RawMessage) (interface{}, error) {
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
	if req.Strategy == "" {
		req.Strategy = "pos-or"
	}

	ctx := context.Background()
	overfetch := safeOverfetch(req.Limit)
	lexHits, _ := my.searcher.SearchLex(req.Query, req.Collection, overfetch, 0, req.Strategy)

	prompt := "根据以下问题，生成一段可能包含答案的假设性段落。" +
		"用与问题相同的语言回答。直接写出段落内容，不要添加任何前缀或说明。\n\n问题：" + req.Query
	messages := []llm.Message{{Role: "user", Content: prompt}}
	hypothetical, err := my.llmProvider.ChatCompletion(ctx, messages)
	if err != nil {
		logo.Info("handleToolHyde: LLM failed, fallback to hybrid: %s", err)
		results := my.fullHybridSearch(ctx, req.Query, req.Collection, req.Limit, req.MinScore, req.Strategy)
		return map[string]interface{}{"hits": results, "hyde_generated": false}, nil
	}

	vecHits, err := my.searcher.SearchVector(ctx, my.embedProvider, hypothetical, req.Collection, overfetch, 0)
	if err != nil {
		results := service.FuseResults(lexHits, nil)
		results = filterAndLimit(results, req.MinScore, req.Limit)
		return map[string]interface{}{"hits": results, "hyde_generated": true}, nil
	}

	results := service.FuseResults(lexHits, vecHits)
	results = filterAndLimit(results, req.MinScore, req.Limit)
	logo.Info("handleToolHyde: query=%q hyde_len=%d results=%d", req.Query, len([]rune(hypothetical)), len(results))
	return map[string]interface{}{"hits": results, "hyde_generated": true}, nil
}
