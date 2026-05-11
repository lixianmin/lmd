package embedding

import (
	"context"
	"os"
	"testing"
)

func TestEmbedQuality_SummaryVsFullDoc(t *testing.T) {
	baseURL := os.Getenv("OLLAMA_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run embedding quality tests")
	}

	p := NewOllamaProvider(baseURL, "batiai/qwen3-embedding:0.6b")
	defer p.Close()
	ctx := context.Background()

	query := "什么是原子性，跟事务有什么关系"

	summaries := []struct {
		label   string
		summary string
	}{
		{"事务ACID文档摘要", "该文档介绍了数据库事务的ACID特性，包括原子性、一致性、隔离性和持久性的概念和实现原理。"},
		{"NIO文档摘要", "该文档系统介绍了 Java NIO 的 Channel、ByteBuffer 及 FileChannel 等核心组件，详解了其非阻塞 IO 模式、缓冲机制及文件随机读写特性。"},
		{"PostgreSQL索引摘要", "该文档系统介绍了PostgreSQL的多种索引类型，包括BTree、Hash、Gin、Brin、Bloom等。内容涵盖了各类索引的技术原理、适用场景及性能对比。"},
		{"Unity着色器摘要", "本文档系统阐述了 Unity 着色器管线流程，包括顶点与片段着色器的 MVP 变换、纹理贴图的物理属性及光照模型，重点讲解了 PBR 渲染原理与性能优化技巧。"},
	}

	queryVec, err := p.EmbedQuery(ctx, query)
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	texts := make([]string, len(summaries))
	for i, s := range summaries {
		texts[i] = s.summary
	}
	docVecs, err := p.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	type scored struct {
		label string
		score float64
	}
	var results []scored
	for i, dv := range docVecs {
		results = append(results, scored{summaries[i].label, cosineSimilarity(queryVec, dv)})
	}

	t.Logf("Query: %q", query)
	for _, r := range results {
		t.Logf("  %s: %.4f", r.label, r.score)
	}

	if results[0].label != "事务ACID文档摘要" {
		t.Errorf("expected top result to be '事务ACID文档摘要', got %q", results[0].label)
	}
}
