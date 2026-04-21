package service

import (
	"github.com/lixianmin/lmd/internal/formatter"
)

func FuseResults(lexHits, vecHits []formatter.SearchHit) []formatter.SearchHit {
	lists := [][]formatter.SearchHit{lexHits, vecHits}
	return ReciprocalRankFusion(lists, DefaultRRFParams())
}
