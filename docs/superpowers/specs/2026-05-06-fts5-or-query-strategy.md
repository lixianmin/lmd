# FTS5 OR Query Strategy — 参考 VBFS agent-memory-store

## 问题

`/query` 端点的 `SearchLex` 将 tokenize 后的原始查询直接传给 FTS5 的 `MATCH`：

```
"what degree did I graduate with" → MATCH 'what degree did I graduate with'
```

FTS5 暗含 AND 语义：多个词必须同时出现在同一个文档中。英语长查询几乎全部返回 0 结果，导致 hybrid search 退化为纯向量搜索。LongMemEval benchmark 的 hybrid Recall@5 仅 49.1%。

## 参考：VBFS agent-memory-store

GitHub: [vbfs/agent-memory-store](https://github.com/vbfs/agent-memory-store)
NPM: [@iflow-mcp/vbfs-agent-memory-store](https://www.npmjs.com/package/@iflow-mcp/vbfs-agent-memory-store)

LongMemEval 成绩：BM25 92.0% / Hybrid 92.1% / Semantic 86.1%（全部 Recall@5）。

### 其 FTS5 查询策略

```javascript
// benchmarks/lib/bench-store.js — searchFTS()
const ftsQuery = query
    .replace(/[^a-zA-Z0-9\s]/g, " ")  // 1. 只保留字母数字和空格
    .split(/\s+/)                       // 2. 按空白分词
    .filter((t) => t.length > 1)        // 3. 去掉单字母词
    .join(" OR ");                      // 4. OR 连接
```

关键差异：**显式 OR 替代 FTS5 暗含 AND**。每个问题一次 FTS 查询，零额外开销。

### 其 RRF 融合策略

```javascript
// wBM25=0.4, wVec=0.6 (K=60)
// 与 LMD 的 2x primary weight 不同，但方向一致
```

## 设计

### 改动

`internal/service/searcher.go` — `SearchLex`

| 改前 | 改后 |
|------|------|
| tokenizer 分词 → 传原始字符串给 FTS | 去非字母数字 → 按空白分词 → 去单字 → OR 连接 |
| 暗含 AND，英语长句全死 | 显式 OR，每个词独立匹配 |

### 新函数 `buildFTSQuery(raw string) string`

```
"what degree did I graduate with?"
  → [what, degree, did, i, graduate, with]  // 去标点、分词
  → [what, degree, did, graduate, with]      // 去单字 "i"
  → "what OR degree OR did OR graduate OR with"
```

CJK 文本同样适用（中文 tokenizer 已将词用空格分隔）。

## 验证

1. `make test` 全部通过
2. 直接 SQLite 测试：
   ```
   MATCH 'what OR degree OR did OR graduate OR with' → 有结果
   MATCH 'what degree did I graduate with'               → 0 结果（旧）
   ```
3. LongMemEval hybrid benchmark — 预期 Recall@5 从 49% 提升至接近 VBFS BM25 的 92%

## 来源

- VBFS benchmark 源码：<https://raw.githubusercontent.com/vbfs/agent-memory-store/main/benchmarks/lib/bench-store.js>
- VBFS LongMemEval 成绩：<https://www.npmjs.com/package/@iflow-mcp/vbfs-agent-memory-store>
