package service

import (
	"fmt"

	"github.com/lixianmin/lmd/internal/dao"
)

type MemorySearchResult struct {
	ID         int64  `json:"id"`
	Content    string `json:"content"`
	Collection string `json:"collection"`
	CreatedAt  string `json:"created_at"`
}

type MemoryService struct{}

func NewMemoryService() *MemoryService {
	return &MemoryService{}
}

func (my *MemoryService) Add(content string) (int64, error) {
	if content == "" {
		return 0, fmt.Errorf("content is required")
	}
	return dao.InsertMemory(content)
}

func (my *MemoryService) Delete(id int64) error {
	return dao.DeleteMemory(id)
}

func (my *MemoryService) Update(id int64, content string) error {
	if content == "" {
		return fmt.Errorf("content is required")
	}
	return dao.UpdateMemory(id, content)
}

func (my *MemoryService) List(limit int) ([]MemorySearchResult, error) {
	recs, err := dao.ListMemories(limit)
	if err != nil {
		return nil, err
	}
	results := make([]MemorySearchResult, len(recs))
	for i, r := range recs {
		results[i] = MemorySearchResult{
			ID:         r.Id,
			Content:    r.Content,
			Collection: r.Collection,
			CreatedAt:  r.CreatedAt.Format("2006-01-02 15:04:05"),
		}
	}
	return results, nil
}
