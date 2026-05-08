package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/logo"
)

const topicTopK = 3

type TopicRouter struct{}

func NewTopicRouter() *TopicRouter {
	return &TopicRouter{}
}

func (my *TopicRouter) Route(queryVec []float32) ([]string, map[int64]bool, error) {
	if len(queryVec) == 0 {
		return nil, nil, nil
	}

	vecResults, err := dao.QueryTopicVectors(queryVec, topicTopK)
	if err != nil {
		return nil, nil, err
	}

	if len(vecResults) == 0 {
		return nil, nil, nil
	}

	collections := make(map[string]bool)
	docIDSet := make(map[int64]bool)

	for _, r := range vecResults {
		topic, err := dao.GetTopicByRowID(r.TopicRowID)
		if err != nil {
			logo.Warn("TopicRouter: get topic by rowID %d failed: %s", r.TopicRowID, err)
			continue
		}
		collections[topic.Collection] = true

		var docPaths []string
		if err := json.Unmarshal([]byte(topic.DocPaths), &docPaths); err != nil {
			logo.Warn("TopicRouter: parse doc_paths failed for %s/%s: %s", topic.Collection, topic.RelPath, err)
			continue
		}

		for _, p := range docPaths {
			fullPath := topic.RelPath
			if fullPath != "" && !strings.HasSuffix(fullPath, "/") {
				fullPath += "/"
			}
			fullPath += p
			doc, err := dao.GetDocumentByPath(topic.Collection, fullPath)
			if err != nil {
				continue
			}
			docIDSet[doc.Id] = true
		}
	}

	var colList []string
	for c := range collections {
		colList = append(colList, c)
	}
	logo.Info("TopicRouter: matched %d topics → %d collections, %d docs",
		len(vecResults), len(colList), len(docIDSet))
	return colList, docIDSet, nil
}

func (my *TopicRouter) SearchInDocs(searcher *Searcher, provider embedding.EmbeddingProvider, query string, docIDs map[int64]bool, limit int, strategy string) ([]formatter.SearchHit, error) {
	fetchLimit := limit * 5
	if fetchLimit > 500 {
		fetchLimit = 500
	}

	lexHits, lexErr := searcher.SearchLex(query, "", fetchLimit, 0, strategy)
	if lexErr != nil {
		logo.Warn("TopicRouter: SearchLex failed: %s", lexErr)
	}

	vecHits, vecErr := searcher.SearchVector(context.Background(), provider, query, "", fetchLimit, 0)
	if vecErr != nil {
		logo.Warn("TopicRouter: SearchVector failed: %s", vecErr)
	}

	lexHits = filterHitsByDocIDs(lexHits, docIDs)
	vecHits = filterHitsByDocIDs(vecHits, docIDs)

	results := FuseResults(lexHits, vecHits)
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func filterHitsByDocIDs(hits []formatter.SearchHit, docIDs map[int64]bool) []formatter.SearchHit {
	if len(docIDs) == 0 {
		return hits
	}
	if len(hits) == 0 {
		return hits
	}
	chunkIDs := make([]int64, len(hits))
	for i, h := range hits {
		chunkIDs[i] = h.ChunkId
	}
	chunks, err := dao.GetChunksByIds(chunkIDs)
	if err != nil {
		return hits
	}
	chunkDocMap := make(map[int64]int64, len(chunks))
	for _, c := range chunks {
		chunkDocMap[c.Id] = c.DocId
	}
	var filtered []formatter.SearchHit
	for _, h := range hits {
		docID, ok := chunkDocMap[h.ChunkId]
		if ok && docIDs[docID] {
			filtered = append(filtered, h)
		}
	}
	return filtered
}
