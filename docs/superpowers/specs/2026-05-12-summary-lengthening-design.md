# Summary 加长设计

## 背景

当前 summary prompt 要求 1-2 句话、不超过 100 字，信息密度不足，影响 document-level 检索精度。需要适度加长 summary，覆盖更多关键信息。

## 改动

共 2 处：

1. **`internal/config/config.go:78`** — `SummaryConfig.MaxOutputTokens` 默认值从 512 改为 768
2. **`internal/service/processor.go:129`** — prompt 从 `用1-2句话(不超过100字)概括其内容和核心主题` 改为 `用3-5句话(200-300字)概括其内容、主要论点和结论`

## 不改的部分

- chunk size（300 runes）不变
- truncateContent 逻辑不变（maxInput 30000 足够）
- summary 存储（per-document，`@summaries` collection）不变
- embedding 模型不变（qwen3-embedding:0.6b，max input ~8192 tokens，200-300 字 summary 约 400-600 tokens，远低于上限）

## 验证

对一个已有文档重新生成 summary，确认输出在 200-300 字范围内。
