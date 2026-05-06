# Search Strategy Tasks

> Spec: `docs/superpowers/specs/2026-05-06-search-query-strategy-evolution.md`

## Task 1: DF 策略用 GSE 内置 IDF 替代 SQL COUNT

**现状**：`buildFTSQueryDF()` 每个查询词执行一次 `SELECT COUNT(*) FROM chunks_fts WHERE MATCH ?`，5 词 = 5 次 SQL。

**目标**：读取 GSE 内置的 `idf.txt`（已在 vendor 中，352k 行，预计算 IDF 值），直接查表替代 SQL。

**文件**：
- `internal/tokenizer/gse.go` — 添加 `GetIDF(word string) float64` 方法
- `internal/service/searcher.go` — `buildFTSQueryDF()` 调用 `tokenizer.GetIDF()` 替代 `dao.GetTermCount()`

**预期效果**：
- DF 速度从 13.1 q/s → ~20+ q/s（减少 5 次 SQL 往返）
- 准确率可能变化（GSE IDF 是基于通用语料，不是 LongMemEval 语料）

## Task 2: 中文停用词从 GSE 加载

**现状**：手动维护 ~50 个中文停用词在 `stopWords` map 中。

**目标**：从 GSE 的 `stop_tokens.txt`（vendor 中）加载，自动补齐中文停用词。

**文件**：
- `internal/tokenizer/gse.go` — 添加 `IsStopWord(word string) bool` 方法
- `internal/service/searcher.go` — `isStopWord()` 调用 tokenizer 接口

**注意**：GSE 的 `stop_tokens.txt` 约 900 个词，远多于我们手动维护的 50 个。

## Task 3: Top-50 → Top-5 关键词重叠重排（后续）

**想法**：BM25 的 `rank` 排序不是最优。可以多取 50 个结果，用关键词重叠度重新排序，取 top 5。

**参考**：QMD `hybridSearch()` 第 5 步 — `keyword-best-chunk selection`

**预期效果**：提升准确率 3-5%，但增加少量计算。

**备注**：本次不实施，记录为后续优化方向。
