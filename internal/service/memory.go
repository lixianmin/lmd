package service

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/embedding"
	"github.com/lixianmin/lmd/internal/tokenizer"
	"github.com/lixianmin/logo"
)

var cst8 = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

const forgetThreshold = 0.05 // 衰变后分数低于此阈值的 Episode 被遗忘

const (
	decayBase              = 0.5  // 指数衰变底数：每过半衰期分数减半
	episodeHalfLife        = 15.0 // episode 类型记忆半衰期（天）
	relationRecencyBase    = 0.7  // Relation 时效偏好底数，保证老记录不低于相关性的 70%
	relationRecencyHalfLife = 365.0 // Relation 时效偏好半衰期（天），缓慢偏好新记录
)

type MemorySearchResult struct {
	ID        int64   `json:"id"`
	Content   string  `json:"content"`
	Type      string  `json:"type"`
	Score     float64 `json:"score"`
	RawScore  float64 `json:"-"`
	CreatedAt string  `json:"created_at"`
}

type MemoryService struct {
	tokenizer tokenizer.Tokenizer
	provider  embedding.EmbeddingProvider
}

func NewMemoryService(tok tokenizer.Tokenizer, prov embedding.EmbeddingProvider) *MemoryService {
	return &MemoryService{tokenizer: tok, provider: prov}
}

func (my *MemoryService) Add(content, memType string) (int64, error) {
	switch memType {
	case "fact", "episode", "relation":
	default:
		return 0, fmt.Errorf("invalid memory type: %q (must be fact, episode, or relation)", memType)
	}

	return dao.InsertMemory(content, memType)
}

func (my *MemoryService) Query(query string, limit int) ([]MemorySearchResult, error) {
	ftsQuery := query
	if my.tokenizer != nil {
		tokenized := my.tokenizer.TokenizeToString(query)
		if tokenized != "" {
			ftsQuery = tokenized
		}
	}

	ftsRecords, err := dao.SearchMemoryFTS(ftsQuery, limit*3)
	if err != nil {
		return nil, err
	}

	var ftsItems []RankedItem
	for _, r := range ftsRecords {
		ftsItems = append(ftsItems, RankedItem{Key: r.Id})
	}

	var vecRecords []dao.MemoryRecord
	if my.provider != nil {
		vec, embedErr := my.provider.Embed(context.Background(), query)
		var vecErr error
		if embedErr == nil {
			vecRecords, vecErr = dao.SearchMemoryVector(vec, limit*3)
			if vecErr != nil {
				logo.Warn("MemoryService.Query: vector search failed: %s", vecErr)
			}
		}
	}

	var vecItems []RankedItem
	for _, r := range vecRecords {
		vecItems = append(vecItems, RankedItem{Key: r.Id})
	}

	ranked := ReciprocalRankFusionGeneric([][]RankedItem{ftsItems, vecItems}, DefaultRRFParams())

	recordMap := make(map[int64]dao.MemoryRecord)
	for _, r := range ftsRecords {
		recordMap[r.Id] = r
	}
	for _, r := range vecRecords {
		if _, ok := recordMap[r.Id]; !ok {
			recordMap[r.Id] = r
		}
	}

	now := time.Now().In(cst8)
	var results []MemorySearchResult
	for _, r := range ranked {
		rec := recordMap[r.Key]
		ageDays := now.Sub(rec.CreatedAt).Hours() / 24
		decay := timePenalty(rec.Type, ageDays)
		finalScore := r.Score * decay

		if rec.Type == "episode" && finalScore < forgetThreshold {
			continue
		}

		results = append(results, MemorySearchResult{
			ID:        rec.Id,
			Content:   rec.Content,
			Type:      rec.Type,
			Score:     finalScore,
			RawScore:  r.Score,
			CreatedAt: rec.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// timePenalty 根据记忆类型返回时间修正因子：
// - Fact: 永远 1.0，时间不影响
// - Episode: 指数衰减，过期的被遗忘
// - Relation: 轻微时效偏好（recencyBoost），永不遗忘
func timePenalty(memType string, ageDays float64) float64 {
	switch memType {
	case "fact":
		return 1.0
	case "episode":
		return math.Pow(decayBase, ageDays/episodeHalfLife)
	case "relation":
		return relationRecencyBase + (1-relationRecencyBase)*math.Exp(-ageDays*math.Ln2/relationRecencyHalfLife)
	default:
		return 1.0
	}
}
