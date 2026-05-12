package embedding

import (
	"context"
	"fmt"
	"os"
	"testing"
)

func TestEmbedQuality_DiagnoseScore1(t *testing.T) {
	baseURL := os.Getenv("OLLAMA_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1")
	}

	p := NewOllamaProvider(baseURL, "batiai/qwen3-embedding:0.6b", "")
	defer p.Close()
	ctx := context.Background()

	query := "什么是原子性，跟事务有什么关系"
	summary := "本文档系统阐述了 Unity 着色器管线流程，包括顶点与片段着色器的 MVP 变换、纹理贴图（如 Base Color、AO、Normal）的物理属性及光照模型，重点讲解了 PBR 渲染原理与性能优化技巧。"

	queryVec, err := p.EmbedQuery(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	docVec, err := p.Embed(ctx, summary)
	if err != nil {
		t.Fatal(err)
	}

	sim := cosineSimilarity(queryVec, docVec)
	t.Logf("query=%q", query)
	t.Logf("summary=%q", summary)
	t.Logf("similarity=%.6f", sim)

	if sim > 0.9 {
		t.Errorf("similarity %.4f is suspiciously high for unrelated query/doc pair", sim)
	}

	// Also test: embed query twice, check consistency
	queryVec2, _ := p.EmbedQuery(ctx, query)
	simSelf := cosineSimilarity(queryVec, queryVec2)
	t.Logf("query self-similarity=%.6f", simSelf)

	// Test a clearly matching pair
	acidSummary := "该文档介绍了数据库事务的ACID特性，包括原子性、一致性、隔离性和持久性的概念和实现原理。"
	acidVec, _ := p.Embed(ctx, acidSummary)
	simAcid := cosineSimilarity(queryVec, acidVec)
	t.Logf("ACID summary similarity=%.6f", simAcid)

	fmt.Printf("Unity=%.4f ACID=%.4f Self=%.4f\n", sim, simAcid, simSelf)
}
