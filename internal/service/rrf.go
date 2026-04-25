package service

import (
	"sort"

	"github.com/lixianmin/lmd/internal/formatter"
)

type RankedItem struct {
	Key   int64
	Score float64
}

type RRFParams struct {
	K             float64
	Weights       []float64
	TopRankBonus1 float64
	TopRankBonus2 float64
}

func DefaultRRFParams() RRFParams {
	return RRFParams{
		K:             rrfK,
		TopRankBonus1: rrfTopRankBonus1,
		TopRankBonus2: rrfTopRankBonus2,
	}
}

const (
	rrfK             = 60.0 // RRF 平滑常数，防止排名靠前的项目过度主导
	rrfTopRankBonus1 = 0.05 // 排名第 1 的额外加分
	rrfTopRankBonus2 = 0.02 // 排名第 2-3 的额外加分
	rrfPrimaryWeight = 2.0  // 前两个检索列表（BM25 + 向量）的权重倍数
	rrfPrimaryCount  = 2    // 享受 primary weight 的列表数量
)

func ReciprocalRankFusionGeneric(lists [][]RankedItem, params RRFParams) []RankedItem {
	type entry struct {
		item     RankedItem
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
		} else if i < rrfPrimaryCount {
			weight = rrfPrimaryWeight
		} else {
			weight = 1.0
		}

		for r, item := range list {
			contribution := weight / (params.K + float64(r) + 1)
			if existing, ok := scores[item.Key]; ok {
				existing.score += contribution
				if r < existing.bestRank {
					existing.bestRank = r
				}
			} else {
				scores[item.Key] = &entry{
					item:     item,
					score:    contribution,
					bestRank: r,
				}
			}
		}
	}

	for _, e := range scores {
		if e.bestRank == 0 {
			e.score += params.TopRankBonus1
		} else if e.bestRank <= rrfPrimaryCount {
			e.score += params.TopRankBonus2
		}
	}

	results := make([]RankedItem, 0, len(scores))
	for _, e := range scores {
		results = append(results, e.item)
	}

	sort.Slice(results, func(i, j int) bool {
		ki := results[i].Key
		kj := results[j].Key
		if scores[ki].score != scores[kj].score {
			return scores[ki].score > scores[kj].score
		}
		return ki < kj
	})

	maxScore := 0.0
	for _, e := range scores {
		if e.score > maxScore {
			maxScore = e.score
		}
	}
	for i := range results {
		if maxScore > 0 {
			results[i].Score = scores[results[i].Key].score / maxScore
		}
	}

	return results
}

func ReciprocalRankFusion(lists [][]formatter.SearchHit, params RRFParams) []formatter.SearchHit {
	var genericLists [][]RankedItem
	for _, list := range lists {
		var items []RankedItem
		for _, h := range list {
			items = append(items, RankedItem{Key: h.ChunkId})
		}
		genericLists = append(genericLists, items)
	}

	ranked := ReciprocalRankFusionGeneric(genericLists, params)

	scoreMap := make(map[int64]float64, len(ranked))
	for _, r := range ranked {
		scoreMap[r.Key] = r.Score
	}

	hitMap := make(map[int64]formatter.SearchHit)
	for _, list := range lists {
		for _, h := range list {
			if existing, ok := hitMap[h.ChunkId]; !ok || existing.Snippet == "" && h.Snippet != "" {
				hitMap[h.ChunkId] = h
			}
		}
	}

	results := make([]formatter.SearchHit, 0, len(ranked))
	for _, r := range ranked {
		hit := hitMap[r.Key]
		hit.Score = r.Score
		results = append(results, hit)
	}

	return results
}
