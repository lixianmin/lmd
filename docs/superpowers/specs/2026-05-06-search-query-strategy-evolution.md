# Search Query Strategy Evolution

## 背景

`/search` 和 `/query` 端点的 FTS5 查询策略经历了多次迭代，每次参考不同项目并针对 LongMemEval benchmark 的实测结果调整。

### 原始状态（2026-05-05 之前）

```
用户查询 → gse tokenizer 分词 → 直接传给 FTS5 MATCH
```

问题：FTS5 隐含 AND 语义。英语长查询如 "What degree did I graduate with?" 中 6 个词全部 AND，没有单个 chunk 同时包含所有词 → 返回 0 结果。

Bug 发现：`?`、`$` 等字符触发 FTS5 syntax error → 500 错误。

### 第一次修复：FTS 安全字符过滤 + VBFS OR 策略

参考：[VBFS agent-memory-store](https://github.com/vbfs/agent-memory-store)
文件：`benchmarks/lib/bench-store.js` — `searchFTS()`

VBFS 的做法：
1. 去除非字母数字字符
2. 按空白分词
3. 去掉单字母词（`length > 1`）
4. 显式 OR 连接

改动：
- `SearchLex` 拆出 `buildFTSQuery()` 函数
- `ftsSafeRe` 保留字母数字 + CJK + 空白
- OR 语义替代暗含 AND

效果：`lex=0` → `lex=90`，FTS 查询不再全死。

### 第二次优化：DF 稀有词提取

参考：LMD 自带的 Go benchmark (`benchmarks/longmemeval/main.go` — `buildQuery()`)

Go benchmark 的做法（92.2% Recall@5）：
1. 每个查询词计算 DF（`SELECT COUNT(*) FROM chunks_fts WHERE MATCH ?`）
2. 按 DF 升序排列（最稀有在前）
3. 取 top 5 做 OR 查询

改动：添加 `buildFTSQueryDF()` 函数 + `strategy` 参数（`or` / `df`）

实测（LongMemEval 全局搜索，150 题，97 万 chunk）：

| 策略 | R@5 | 速度 |
|------|-----|------|
| OR  | 33.3% | 12.8 q/s |
| DF top5 | 31.5% | 13.1 q/s |

结论：OR 比 DF 准确，DF 略快但无显著优势。DF 的瓶颈是每个查询 5 次额外 SQL。

### 第三次优化：中英文停用词 + CJK 分词恢复

改动：
1. OR 策略添加中英文停用词过滤（~120 词）
2. DF top 8 → top 5（匹配 VBFS 的参数）
3. 恢复 `gse` CJK 分词（`TokenizeToString`）

效果：OR R@5 从 32.2% → 33.3%（CJK 分词微幅提升）

### 第四次尝试：QMD AND + BM25 策略（失败）

参考：[QMD](https://github.com/xmli/qmd) (`src/store.ts` — `buildFTS5Query`)

QMD 的做法：
1. AND 连接（不是 OR）
2. 每个词加前缀 `*`（FTS5 prefix match）
3. 不去停用词
4. 使用 `bm25(chunks_fts, 1.5, 4.0, 1.0)` 评分

改动：添加 `buildFTSQueryAND()` + `SearchFTSBM25()` + `strategy="and"`

实测：**R@5 = 0%**（LongMemEval），**速度 = 5.0 q/s**

AND 失败原因：LongMemEval 的问题都是长句（10-20 词），AND 要求所有词共现于同一个 chunk。300 rune 的 chunk 太小，无法同时包含 5+ 个查询词。

QMD 能用的原因：搜索的是用户自己的技术文档，查询词典型 1-3 个（"性能优化"、"API设计"），且文档 chunk 更大（未限制 300 rune）。

### 当前状态（2026-05-06）

**已实现：**
- `strategy="or"` — VBFS 风格 OR + 停用词 + CJK 分词
- `strategy="df"` — top 5 稀有词（每词 1 次 SQL COUNT）
- `strategy="and"` — QMD 风格 AND + bm25()（保留做参考，不在生产环境使用）
- `ftsSafeRe` — FTS5 安全字符过滤
- CJK 分词 — gse `TokenizeToString`

**待优化：**

| # | 内容 | 来源 | 优先级 |
|---|------|------|--------|
| 1 | DF 用 GSE 内置 IDF 替代 SQL COUNT | GSE `idf.txt` | 高 |
| 2 | 中文停用词从 GSE `stop_tokens.txt` 加载 | GSE | 高 |
| 3 | Top-50 → Top-5 重排（关键词重叠度） | QMD hybrid 第5步 | 中 |
| 4 | AND 策略移除（生产无价值） | — | 低 |

## 参考项目

| 项目 | 策略 | LongMemEval R@5 | 参考价值 |
|------|------|-----------------|---------|
| VBFS agent-memory-store | OR + 去单字 | 92.0% BM25 | OR 查询构建 |
| LMD Go benchmark | DF + OR | 92.2% BM25 | DF 稀有词提取 |
| QMD | AND + bm25() + prefix* | N/A | AND 语义、bm25() 评分 |
| GSE | 内置 IDF 词典 + 停用词 | N/A | 词频查询替代 |

## 设计决策记录

1. **默认策略选 OR**：准确率最高（33.3%），速度第二（12.8 q/s），实现最简单
2. **保留 DF 策略**：虽准确率略低，但在特定场景（短查询、高 precision 需求）有效
3. **AND 策略仅代码保留**：LongMemEval 全死，但 bm25() 评分和 prefix* 语法有价值参考
4. **CJK 分词必要**：gse 分词微幅提升 OR 策略（32.2% → 33.3%）
