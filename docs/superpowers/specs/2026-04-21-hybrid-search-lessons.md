# Hybrid Search 经验教训

## 日期
2026-04-21

## 背景

为 `lmd query`（混合搜索）设计管道时，叠加了 Rocchio PRF + RRF 融合 + MMR 三个算法，导致 BM25 的好结果被完全覆盖。

## 问题现象

- `lmd search "docker命令"` → 5 条 Docker 相关结果（BM25 正确）
- `lmd vsearch "docker命令"` → 5 条银行流水（向量搜索垃圾）
- `lmd query "docker命令"` → 5 条银行流水（混合搜索完全丢失了 BM25 结果）

## 根因分析

### 1. 算法叠加强不代表效果好

三个算法各自为不同场景设计，不应叠加：

| 算法 | 设计场景 | 叠加后的问题 |
|------|---------|------------|
| Rocchio PRF | 改进单次向量检索 | 在混合系统中与 RRF 重复计算 BM25 信号 |
| RRF 融合 | 合并两个独立列表 | 正确做法 |
| MMR | 纯向量搜索结果多样性 | 用 embedding 相似度重排，直接丢弃 RRF 融合分数和 BM25 贡献 |

### 2. MMR 推翻 RRF 是致命问题

```
BM25 → 15 条好结果
向量 → 15 条垃圾
RRF 融合 → 30 条，好结果排名靠前 ✓
MMR → 从 30 条中选 5 条，只看 embedding 相似度 → 全选了垃圾 ✗
```

OpenSearch 文档明确说："MMR 只能在融合之前用于向量搜索结果，不能用于混合查询"。

### 3. RRF Score = 1/rank 无意义

原实现 `results[i].Score = 1.0 / float64(i+1)` 让第 1 名 Score=1.0，不反映相关性，只是名次倒数。用户看到 Score: 1 会误以为"完美匹配"。

## 业界最佳实践（2024-2025）

### 标准方案

**Elasticsearch、Qdrant、Weaviate、Milvus、MongoDB Atlas 全部采用 RRF：**

```
BM25 独立搜索 ──→ RRF 融合（k=60）──→ 返回 top-N
向量独立搜索 ──→
```

### 不应该用的

- **Rocchio PRF**：在已有 BM25 + 向量双信号的混合系统中，增益递减但增加复杂度
- **MMR**：只适合纯向量搜索，融合后不适用
- **加权分数融合**：BM25 分数（0~30+）和 cosine（0~1）尺度不同，不如 RRF 稳健

## QMD 参考项目的做法

QMD 使用更复杂的 8 步管道，但核心仍是 RRF：

1. BM25 探测 → 如果 top score ≥ 0.85 且领先 ≥ 0.15，跳过扩展
2. LLM 查询扩展（fine-tuned 1.7B 模型，生成 lex/vec/hyde 三种变体）
3. 并行多信号搜索（FTS + 向量）
4. RRF 融合（k=60，前两个列表 2x 权重 + top-rank bonus）
5. Best chunk 选择（按关键词重叠度）
6. LLM Reranking（Qwen3-Reranker-0.6B）
7. Position-aware 分数混合（rank 1-3: 75% RRF + 25% reranker）
8. 去重 + 过滤

**关键区别**：QMD 不用 Rocchio PRF，不用 MMR。用 LLM reranker 而非 MMR 做最终排序。

## 最终方案

```
BM25 + 向量搜索（并行独立）→ RRF 融合（k=60）→ 返回
```

- Score 用 `RRF 分数 / 最高分` 归一化到 0~1
- 不用 PRF、不用 MMR
- 未来可选：加 reranker（Qwen3-Reranker-0.6B）

## 关键教训

1. **简单先于复杂**：业界标配 BM25 + 向量 + RRF 已经够好，不要提前叠加优化
2. **每个算法要理解它的适用场景**：MMR 是向量空间的操作，不适合融合后的列表
3. **融合分数不能被后续步骤覆盖**：RRF 的输出就是最终排名，不需要再排序
4. **实测比理论重要**：理论说 PRF 好、MMR 好，但实际效果要看数据
