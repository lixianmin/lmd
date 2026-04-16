package service

import (
	"sort"

	"github.com/lixianmin/lmd/internal/formatter"
)

func FuseResults(lexHits, vecHits []formatter.SearchHit, vectorWeight float64) []formatter.SearchHit {
	textWeight := 1.0 - vectorWeight

	lexBest := dedupBest(lexHits)
	vecBest := dedupBest(vecHits)

	type scored struct {
		hit   formatter.SearchHit
		score float64
	}

	docs := make(map[string]*scored)

	for _, h := range lexBest {
		docs[h.DocId] = &scored{hit: h, score: textWeight * h.Score}
	}

	for _, h := range vecBest {
		if existing, exists := docs[h.DocId]; exists {
			existing.score += vectorWeight * h.Score
			if h.Snippet != "" && existing.hit.Snippet == "" {
				existing.hit.Snippet = h.Snippet
			}
		} else {
			docs[h.DocId] = &scored{hit: h, score: vectorWeight * h.Score}
		}
	}

	results := make([]formatter.SearchHit, 0, len(docs))
	for _, s := range docs {
		s.hit.Score = s.score
		results = append(results, s.hit)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}

func dedupBest(hits []formatter.SearchHit) []formatter.SearchHit {
	best := make(map[string]formatter.SearchHit)
	for _, h := range hits {
		if existing, ok := best[h.DocId]; !ok || h.Score > existing.Score {
			best[h.DocId] = h
		}
	}
	result := make([]formatter.SearchHit, 0, len(best))
	for _, h := range best {
		result = append(result, h)
	}
	return result
}
