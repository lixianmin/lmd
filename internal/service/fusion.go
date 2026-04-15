package service

import (
	"sort"

	"github.com/lixianmin/lmd/internal/formatter"
)

func ReciprocalRankFusion(lexHits, vecHits []formatter.SearchHit, k int, origWeight float64) []formatter.SearchHit {
	type scored struct {
		hit      formatter.SearchHit
		rrfScore float64
	}

	docs := make(map[string]*scored)

	for rank, h := range lexHits {
		if _, exists := docs[h.DocId]; !exists {
			docs[h.DocId] = &scored{hit: h}
		}
		docs[h.DocId].rrfScore += origWeight / float64(k+rank+1)
	}

	for rank, h := range vecHits {
		if existing, exists := docs[h.DocId]; exists {
			existing.rrfScore += origWeight / float64(k+rank+1)
			if h.Snippet != "" && existing.hit.Snippet == "" {
				existing.hit.Snippet = h.Snippet
			}
		} else {
			docs[h.DocId] = &scored{hit: h}
			docs[h.DocId].rrfScore += origWeight / float64(k+rank+1)
		}
	}

	results := make([]formatter.SearchHit, 0, len(docs))
	for _, s := range docs {
		s.hit.Score = s.rrfScore
		results = append(results, s.hit)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	if len(results) > 0 {
		topScore := results[0].Score
		for i := range results {
			results[i].Score = results[i].Score / topScore
		}
	}

	return results
}
