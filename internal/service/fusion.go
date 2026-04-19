package service

import (
	"github.com/lixianmin/lmd/internal/formatter"
)

func FuseResults(lexHits, vecHits []formatter.SearchHit) []formatter.SearchHit {
	lists := [][]formatter.SearchHit{lexHits, vecHits}
	return ReciprocalRankFusion(lists, DefaultRRFParams())
}

func FuseResultsThree(lexHits, vecHits, hydeHits []formatter.SearchHit) []formatter.SearchHit {
	lists := [][]formatter.SearchHit{lexHits, vecHits, hydeHits}
	params := DefaultRRFParams()
	return ReciprocalRankFusion(lists, params)
}
