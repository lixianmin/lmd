# 检索质量优化：分块、指令前缀、PRF、MMR

## 背景

项目使用 Qwen3-Embedding-0.6B（本地 llama-go）+ BM25 做 Hybrid 检索。当前存在几个影响检索质量的问题：

1. **分块过大（1200 字）** → embedding 语义稀释，短查询难以匹配
2. **Embedding 无指令前缀** → 查询与文档在语义空间中对齐不足
3. **向量查询无扩展** → 短查询的 embedding 信息量不足
4. **结果无去重** → 同一文档的多个相似 chunk 可能占据 top-K

## 一、分块参数调整

### 决策

| 参数 | 旧值 | 新值 | 理由 |
|------|------|------|------|
| chunkSize | 1200 runes | 300 runes | 减少语义稀释，适配 0.6B embedding 模型 |
| hardMax | chunkSize + 300 | chunkSize + 150 | 硬切上限同比缩小 |
| overlapChars | 200 | ~45（15%），以句为单位对齐 | 防止跨句撕裂 |
| minChunkSize | 200 | 80 | 同比缩小，避免合并过多碎片 |

### Overlap 对齐规则

Overlap 不是固定百分比，而是在目标区间内找最近的句号断句：
- 目标：前一个 chunk 末尾 ~15% 内容作为下一个 chunk 的前缀
- 对齐：在目标位置 ±50 字的窗口内，找最近的句末标点（。！？.!?）断句
- 已有 `addOverlap()` 实现了句号检测逻辑，调参即可
- 窗口内无句号 → 不加 overlap（宁可不加也不撕裂）

### 参考文献

社区普遍建议 embedding 场景下 chunk size 200-400 字（中文），overlap 10-20%。300 字是中位值。

### QMD 参考项目对比

QMD 使用 900 tokens（~3600 chars）+ 15% overlap。但 QMD 使用更大模型和更长上下文窗口，我们的 0.6B 模型需要更小的 chunk。

## 二、Qwen3-Embedding 指令前缀（P0）

### 决策

**Query 必须加指令前缀，Document 不加。**

```
# Query embedding 输入格式
Instruct: Given a web search query, retrieve relevant passages that answer the query
Query: {query}

# Document embedding 输入格式
{raw_text}（原文，不加任何前缀）
```

### 来源

Qwen3-Embedding 官方 HuggingFace 文档（https://huggingface.co/Qwen/Qwen3-Embedding-0.6B）：
- 指令用**英文**写，即使内容是中文
- 使用指令可提升检索质量 **1-5%**
- Documents 不需要加指令

### QMD 参考项目对比

QMD 对 Qwen3-Embedding 使用相同模式：
```typescript
// Query: 加 Instruct 前缀
`Instruct: Retrieve relevant documents for the given query\nQuery: ${query}`
// Document: 原文
title ? `${title}\n${text}` : text
```

### 代码改动

`internal/embedding/llama.go` 的 `EmbedQuery` 方法需在 query 前拼接指令前缀。`Embed` 方法（文档 embedding）不变。

```go
const embedQueryPrefix = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery: "

func (my *LlamaProvider) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
    return my.Embed(ctx, embedQueryPrefix+query)
}
```

### 注意事项

- 指令前缀本身也会被 embedding，因此 query 和 doc 的 embedding 维度/空间是一致的
- 前缀约 80 字符，对 32K 上下文窗口可忽略不计
- 此改动会导致已有 embedding 全部失效（query 侧空间变了），需要 `lmd rebuild` 重建索引

## 三、Rocchio Embedding Feedback（P1）

### 决策

采用 **Rocchio 算法**（方案 B）做向量查询扩展，不用 Text PRF（方案 A）。

### Rocchio 公式

```
q_new = α × q_emb + β × mean(top_k_doc_embs)
```

- `q_emb`：原始 query 的 embedding（已含指令前缀）
- `top_k_doc_embs`：BM25 top-k 结果的已有 embedding（预计算，查表即可）
- `α = 0.6`，`β = 0.4`（初始值，可调）
- `k = 3`（取 BM25 top 3）

### 执行流程

```
query
  ├── BM25(raw query) → top 3 chunks → 查已有 embedding → Rocchio blend
  │     ↓
  │   q_new = 0.6 × embed("Instruct:...\nQuery: {query}") + 0.4 × mean(top3_embs)
  │     ↓
  │   向量搜索(q_new)
  │
  └── BM25(raw query) → 原有 BM25 结果
        ↓
      RRF 融合(BM25 结果, 向量搜索结果)
```

### 为什么选 Rocchio 而不是 Text PRF

| 维度 | 方案 A (Text PRF) | 方案 B (Rocchio) |
|------|-------------------|-------------------|
| 额外 embedding 调用 | 1 次 | 0 次 |
| 实现复杂度 | 高（术语提取、分词、去停用词） | 低（向量查表 + 加权平均） |
| 论文支持 | RM3/KL（主要验证在 sparse retrieval） | Rocchio/Average（专门验证在 dense retrieval） |
| 速度 | 慢（要过模型） | 快（纯算术，<1ms） |
| Agent Memory 适配 | 一般 | 好 |

论文来源：Li et al. 2021, ECIR 2022 *"Improving Query Representations for Dense Retrieval with Pseudo Relevance Feedback"*：
> *"Li et al. investigated two simple approaches, Average and Rocchio, to utilise PRF information in dense retrievers without introducing new neural models or further training. Both models achieved superior effectiveness without hurting the efficiency significantly."*

### BM25 无结果的回退策略

**PRF 依赖 BM25 有结果。** 回退逻辑：

```
if BM25 有结果:
    Rocchio 扩展 → 向量搜索
else:
    直接用 query embedding → 向量搜索
```

这与 QMD 的 strong-signal shortcut 思路一致。

### Agent Memory 场景

Agent Memory 条目特点：短（一句话到一段话）、碎片化、关键词明确。

Rocchio 在此场景下更有价值：
1. BM25 搜到 3 条相关 memory → embedding 加权平均 → 移向正确语义邻域
2. 第二轮向量搜索可能找到 BM25 漏掉的、语义更相关但字面不同的 memory
3. 零额外模型调用，对 Agent 延迟敏感场景友好

### 代码改动

1. `internal/service/searcher.go` 的 `SearchVector` 方法：
   - 先调 BM25 获取 top 3 chunks
   - 查这些 chunks 的已有 embedding（从 sqlite-vec）
   - Rocchio blend 后用新 embedding 做向量搜索
2. `internal/dao/` 新增查询 chunk embedding 的函数

## 四、MMR 去重排序（P1）

### 决策

在 RRF fusion 之后、返回结果之前，加 MMR（Maximal Marginal Relevance）后处理。

### MMR 公式

```
MMR(d) = λ × sim(d, q) - (1-λ) × max(sim(d, d') for d' in selected)
```

- `sim(d, q)`：chunk embedding 与 query embedding 的余弦相似度（已有）
- `sim(d, d')`：chunk 之间的余弦相似度（已有 embedding，直接算）
- `λ = 0.7`：偏向相关性，适度去重
- 贪心选择：每次从候选集中选 MMR 最高的加入结果集

### 不用 Rerank 模型的理由

- 本地部署 rerank 模型需要额外加载（资源开销）
- MMR 只用已有 embedding，零额外模型调用
- 对个人知识库和 Agent Memory 场景足够
- 后续如果需要，可独立引入 rerank 模型

### QMD 参考项目对比

QMD 使用 cross-encoder（Qwen3-Reranker-0.6B）做 rerank，不是 MMR。但 QMD 的规模更大，需要更精确的排序。我们的场景下 MMR 更轻量且足够。

### 代码改动

1. `internal/service/` 新增 `mmr.go`：`SelectMMR(candidates, queryVec, lambda, topK)` 函数
2. 在 `SearchHybrid` 的 fusion 结果上调用 MMR

## 五、实施优先级

| 优先级 | 改动 | 成本 | 收益 | 依赖 |
|--------|------|------|------|------|
| P0 | Embedding 指令前缀 | 改 1 个方法 | 1-5% 检索质量 | 需 rebuild 索引 |
| P0 | 分块参数 300 + 句级 overlap | 改参数 | 显著减少语义稀释 | 需 rebuild 索引 |
| P1 | Rocchio Embedding Feedback | 新增 pipeline | 向量查询扩展 | 依赖 P0 |
| P1 | MMR 后处理 | 新增 1 个函数 | 结果多样性 | 独立 |

P0 两个改动都需要 rebuild 索引，建议一起做，只 rebuild 一次。

## 六、参考资料

- **Qwen3-Embedding 官方文档**: https://huggingface.co/Qwen/Qwen3-Embedding-0.6B
- **PRF for Dense Retrieval**: Li et al. 2021, ECIR 2022, arXiv:2108.11044
- **ANCE-PRF**: Yu et al. 2021, CIKM 2021
- **Rocchio Algorithm**: Rocchio 1971, *Relevance feedback in information retrieval*
- **QMD 参考项目**: `/Users/xmli/me/code/others/qmd` — query expansion, RRF fusion, embedding instruction
- **Hermes-Agent 参考项目**: `/Users/xmli/me/code/others/hermes-agent` — Agent Memory 架构（HRR embedding, FTS5, 多策略检索）
