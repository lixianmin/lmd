package embedding

import (
	"context"
	"math"
	"os"
	"testing"
)

func TestEmbedQuality_SemanticRanking(t *testing.T) {
	baseURL := os.Getenv("OLLAMA_URL")
	if baseURL == "" {
		baseURL = "http://localhost:11434"
	}
	if os.Getenv("RUN_INTEGRATION") == "" {
		t.Skip("set RUN_INTEGRATION=1 to run embedding quality tests")
	}

	p := NewOllamaProvider(baseURL, "batiai/qwen3-embedding:0.6b", "")
	defer p.Close()
	ctx := context.Background()

	query := "什么是原子性，跟事务有什么关系"

	docs := []struct {
		label string
		text  string
	}{
		{"数据库事务ACID", "数据库事务具有ACID特性：原子性（Atomicity）要求事务中的所有操作要么全部执行成功，要么全部不执行；一致性（Consistency）确保数据从一个一致状态转换到另一个一致状态；隔离性（Isolation）保证并发事务互不干扰；持久性（Durability）确保提交后的数据不会丢失。"},
		{"Java NIO", "Java NIO（New I/O）是从Java 1.4开始引入的I/O API，提供了不同于标准I/O的文件工作方式。NIO的核心组件包括Channel、Buffer和Selector。Channel是双向的数据通道，Buffer是数据的容器，Selector用于多路复用。"},
		{"PostgreSQL索引", "PostgreSQL支持多种索引类型：BTree索引适用于等值查询和范围查询，Hash索引只适用于等值查询，GIN索引适用于全文搜索和数组查询，BRIN索引适用于大表的有序列。"},
		{"Unity着色器", "Unity ShaderLab中的Surface Shader通过vertex和fragment阶段实现空间坐标转换。Standard渲染管线使用PBR（基于物理的渲染）模型，通过Base Color、Metallic、Smoothness等属性控制材质外观。"},
		{"Kafka部署", "Apache Kafka可以通过Docker容器化部署，也可以直接安装在服务器上。Kafka集群由多个broker组成，每个broker负责存储一部分分区数据。生产者将消息发送到指定topic，消费者从topic订阅消息。"},
	}

	queryVec, err := p.EmbedQuery(ctx, query)
	if err != nil {
		t.Fatalf("EmbedQuery failed: %v", err)
	}

	docVecs, err := p.EmbedBatch(ctx, []string{docs[0].text, docs[1].text, docs[2].text, docs[3].text, docs[4].text})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}

	type scored struct {
		label string
		score float64
	}
	var results []scored
	for i, dv := range docVecs {
		results = append(results, scored{docs[i].label, cosineSimilarity(queryVec, dv)})
	}

	t.Logf("Query: %q", query)
	for _, r := range results {
		t.Logf("  %s: %.4f", r.label, r.score)
	}

	if results[0].label != "数据库事务ACID" {
		t.Errorf("expected top result to be '数据库事务ACID', got %q (score=%.4f)", results[0].label, results[0].score)
	}
}

func cosineSimilarity(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
