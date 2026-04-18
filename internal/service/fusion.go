package service

import (
	"sort"

	"github.com/lixianmin/lmd/internal/formatter"
)

func FuseResults(lexHits, vecHits []formatter.SearchHit, vectorWeight float64) []formatter.SearchHit {
	textWeight := 1.0 - vectorWeight

	type scored struct {
		hit       formatter.SearchHit
		score     float64
		hasText   bool
		hasVector bool
	}

	chunks := make(map[int64]*scored)

	for _, h := range lexHits {
		chunks[h.ChunkId] = &scored{hit: h, score: textWeight * h.Score, hasText: true}
	}

	for _, h := range vecHits {
		if existing, exists := chunks[h.ChunkId]; exists {
			existing.score += vectorWeight * h.Score
			existing.hasVector = true
			if h.Snippet != "" && existing.hit.Snippet == "" {
				existing.hit.Snippet = h.Snippet
			}
		} else {
			chunks[h.ChunkId] = &scored{hit: h, score: vectorWeight * h.Score, hasVector: true}
		}
	}

	results := make([]formatter.SearchHit, 0, len(chunks))
	for _, s := range chunks {
		s.hit.Score = s.score
		results = append(results, s.hit)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results
}
