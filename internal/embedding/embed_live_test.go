package embedding

import (
	"context"
	"os"
	"testing"

	"github.com/lixianmin/lmd/internal/dao"
)

func TestEmbedQuality_LiveVectorSearch(t *testing.T) {
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1")
	}

	if err := dao.Init(os.Getenv("HOME") + "/.cache/lmd/index.sqlite"); err != nil {
		t.Fatal(err)
	}
	defer dao.DB.Close()

	ollamaURL := os.Getenv("OLLAMA_URL")
	if ollamaURL == "" {
		ollamaURL = "http://localhost:11434"
	}
	p := NewOllamaProvider(ollamaURL, "batiai/qwen3-embedding:0.6b")
	defer p.Close()

	ctx := context.Background()
	query := "什么是原子性，跟事务有什么关系"
	queryVec, err := p.EmbedQuery(ctx, query)
	if err != nil {
		t.Fatal(err)
	}

	results, err := dao.QueryVectorsByCollection(queryVec, "@summaries", 5)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Query: %q", query)
	for i, r := range results {
		chunk, _ := dao.GetChunkById(r.ChunkId)
		score := 1.0 - r.Distance
		content := chunk.Content
		if len(content) > 100 {
			content = content[:100] + "..."
		}
		t.Logf("[%d] chunkId=%d score=%.4f dist=%.4f: %s", i, r.ChunkId, score, r.Distance, content)
	}

	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	topScore := 1.0 - results[0].Distance
	if topScore > 0.95 {
		chunk, _ := dao.GetChunkById(results[0].ChunkId)
		t.Errorf("suspiciously high score %.4f for chunk %d: %s", topScore, results[0].ChunkId, chunk.Content[:80])
	}
}
