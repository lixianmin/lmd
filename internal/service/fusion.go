package service

import (
	"github.com/lixianmin/lmd/internal/formatter"
)

func FuseResults(lexHits, vecHits []formatter.SearchHit) []formatter.SearchHit {
	lists := [][]formatter.SearchHit{lexHits, vecHits}
	return ReciprocalRankFusion(lists, DefaultRRFParams())
}

func FuseResultsThree(lexHits, vecHits, hydeHits []formatter.SearchHit) []formatter.SearchHit {
	// RRF weights: lex=2.0, vec=2.0, hyde=1.0 (primary lists get 2x weight)
	lists := [][]formatter.SearchHit{lexHits, vecHits, hydeHits}
	params := DefaultRRFParams()
	return ReciprocalRankFusion(lists, params)
}
