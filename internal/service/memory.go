package service

import (
	"fmt"
	"math"
	"time"

	"github.com/lixianmin/lmd/internal/dao"
	"github.com/lixianmin/lmd/internal/tokenizer"
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
}

func NewMemoryService(tok tokenizer.Tokenizer) *MemoryService {
	return &MemoryService{tokenizer: tok}
}

func (my *MemoryService) Add(content, memType string) (int64, error) {
	switch memType {
	case "fact", "episode", "relation":
	default:
		return 0, fmt.Errorf("invalid memory type: %q (must be fact, episode, or relation)", memType)
	}

	return dao.InsertMemory(content, memType)
}

func (my *MemoryService) Search(query string, limit int, memType string) ([]MemorySearchResult, error) {
	ftsQuery := query
	if my.tokenizer != nil {
		tokenized := my.tokenizer.TokenizeToString(query)
		if tokenized != "" {
			ftsQuery = tokenized
		}
	}

	var records []dao.MemoryRecord
	var err error

	if memType != "" {
		records, err = dao.SearchMemoryFTSByType(ftsQuery, memType, limit)
	} else {
		records, err = dao.SearchMemoryFTS(ftsQuery, limit)
	}
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var results []MemorySearchResult
	for _, rec := range records {
		ageDays := now.Sub(rec.CreatedAt).Hours() / 24
		decay := decayFactor(rec.Type, ageDays)
		finalScore := rec.Score * decay

		results = append(results, MemorySearchResult{
			ID:        rec.ID,
			Content:   rec.Content,
			Type:      rec.Type,
			Score:     finalScore,
			RawScore:  rec.Score,
			CreatedAt: rec.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}

	return results, nil
}

func decayFactor(memType string, ageDays float64) float64 {
	switch memType {
	case "fact":
		return 1.0
	case "episode":
		return math.Pow(0.5, ageDays/15.0)
	case "relation":
		return math.Pow(0.5, ageDays/180.0)
	default:
		return 1.0
	}
}
