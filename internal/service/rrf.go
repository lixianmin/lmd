package service

import (
	"sort"

	"github.com/lixianmin/lmd/internal/formatter"
)

type RRFParams struct {
	K             float64
	Weights       []float64
	TopRankBonus1 float64
	TopRankBonus2 float64
}

func DefaultRRFParams() RRFParams {
	return RRFParams{
		K:             60.0,
		TopRankBonus1: 0.05,
		TopRankBonus2: 0.02,
	}
}

func ReciprocalRankFusion(lists [][]formatter.SearchHit, params RRFParams) []formatter.SearchHit {
	type entry struct {
		hit      formatter.SearchHit
		score    float64
		bestRank int
	}

	scores := make(map[int64]*entry)

	for i, list := range lists {
		if list == nil {
			continue
		}
		var weight float64
		if i < len(params.Weights) {
			weight = params.Weights[i]
		} else if i < 2 {
			weight = 2.0
		} else {
			weight = 1.0
		}

		for r, hit := range list {
			contribution := weight / (params.K + float64(r) + 1)
			if existing, ok := scores[hit.ChunkId]; ok {
				existing.score += contribution
				if r < existing.bestRank {
					existing.bestRank = r
				}
				if existing.hit.Snippet == "" && hit.Snippet != "" {
					existing.hit.Snippet = hit.Snippet
				}
			} else {
				scores[hit.ChunkId] = &entry{
					hit:      hit,
					score:    contribution,
					bestRank: r,
				}
			}
		}
	}

	for _, e := range scores {
		if e.bestRank == 0 {
			e.score += params.TopRankBonus1
		} else if e.bestRank <= 2 {
			e.score += params.TopRankBonus2
		}
	}

	results := make([]formatter.SearchHit, 0, len(scores))
	for _, e := range scores {
		results = append(results, e.hit)
	}

	sort.Slice(results, func(i, j int) bool {
		if scores[results[i].ChunkId].score != scores[results[j].ChunkId].score {
			return scores[results[i].ChunkId].score > scores[results[j].ChunkId].score
		}
		return results[i].ChunkId < results[j].ChunkId
	})

	for i := range results {
		results[i].Score = 1.0 / float64(i+1)
	}

	return results
}
