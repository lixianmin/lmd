package service

import (
	"github.com/lixianmin/lmd/internal/formatter"
)

func FuseResults(lexHits, vecHits []formatter.SearchHit, vectorWeight float64) []formatter.SearchHit {
	lists := [][]formatter.SearchHit{lexHits, vecHits}
	params := DefaultRRFParams()
	return ReciprocalRankFusion(lists, params)
}
