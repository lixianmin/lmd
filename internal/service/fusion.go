package service

import (
	"github.com/lixianmin/lmd/internal/formatter"
	"github.com/lixianmin/logo"
)

func FuseResults(lexHits, vecHits []formatter.SearchHit) []formatter.SearchHit {
	lists := [][]formatter.SearchHit{lexHits, vecHits}
	result := ReciprocalRankFusion(lists, DefaultRRFParams())
	logo.Info("FuseResults: lex=%d vec=%d fused=%d", len(lexHits), len(vecHits), len(result))
	return result
}
