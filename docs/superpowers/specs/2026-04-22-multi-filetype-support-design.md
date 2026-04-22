# 多文件类型支持：TXT + PDF 调研结论

## 背景

lmd 目前只支持 `.md` 文件。用户希望扩展到其他文件类型。

经过调研和讨论，结论如下：

### PDF：不在 lmd 内支持

1. **Go 生态无可用方案**：Go 的 PDF 库（如 `ledongthuc/pdf`）只能提取纯文本，无法保留结构（标题、表格、公式）
2. **高质量转换工具全是 Python**：
   - **MinerU**（opendatalab，25k stars，Apache-2.0）— 中文优先，最佳中文 PDF 转换质量，支持 OCR + 完整结构保留
   - **Marker**（VikParuchuri，23.7k stars，GPL-3.0）— 通用质量最高，深度学习 pipeline
   - **Docling**（IBM，15k stars，MIT）— 模块化，企业级
   - **MarkItDown**（Microsoft，50k stars，MIT）— 但 PDF 转换只是 PyMuPDF 封装，质量基础
   - **Pandoc** — PDF→MD 不是强项，不支持 OCR
3. **架构决策**：PDF 转 Markdown 由外部 Agent/工具完成（推荐 MinerU），转换后的 `.md` 文件由 lmd 正常索引

### TXT：在 lmd 内支持

TXT 是最简单的纯文本格式，无需任何转换，只需让 lmd 能发现和索引 `.txt` 文件。

## 设计

### 1. Glob Pattern 支持多扩展名

**现状**：`collection add --mask` 默认 `**/*.md`，只匹配单个扩展名。

**改动**：支持 `{md,txt}` 风格的多扩展名 glob。

匹配逻辑改为：对 glob pattern 按逗号分隔为多个子 pattern，文件匹配任一子 pattern 即可。

```
示例输入: "**/*.{md,txt}"
解析为: ["**/*.md", "**/*.txt"]
文件 "notes/readme.md" → 匹配 "**/*.md" → 命中
文件 "notes/data.txt"  → 匹配 "**/*.txt" → 命中
```

注意：不使用 Go 标准库的 `filepath.Match` 对 `{}` 的支持（它不支持），而是在应用层拆分。

### 2. 按文件扩展名选择 Chunker

**现状**：`indexer.go` 中硬编码使用 `MarkdownChunker`。

**改动**：根据文件扩展名选择对应的 `Chunker`：

| 扩展名 | Chunker | 行为 |
|--------|---------|------|
| `.md` | `MarkdownChunker` | 感知 Markdown 结构（标题、代码块、段落），滑动窗口切分 |
| `.txt` | `PlainTextChunker` | 无结构感知，纯按 rune 数量切分 + overlap |

`PlainTextChunker` 实现：
- 直接按 `chunkSize`（300 rune）切分
- 15% overlap，对齐到句号/换行符
- 不感知标题、代码块等 Markdown 结构
- 复用现有的 `estimateTokens`、`byteOffsetToLine` 等工具函数

### 3. extractTitle 对 TXT 的适配

**现状**：`extractTitle` 查找第一个 `# heading`，找不到就用文件名（去掉扩展名）。

**改动**：无。TXT 文件不会有 `# heading`，自然 fallback 到文件名作为 title，行为正确。

### 4. CLI 默认值变更

```
lmd collection add /path --name notes
# 默认 mask 从 "**/*.md" 改为 "**/*.{md,txt}"
```

### 5. DB schema 无变更

`collections` 表的 `glob_pattern` 字段已经存储 glob pattern 字符串，无需修改 schema。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `internal/chunker/plain.go` | **新增**：`PlainTextChunker` 实现 |
| `internal/chunker/plain_test.go` | **新增**：`PlainTextChunker` 测试 |
| `internal/chunker/markdown_test.go` | 无改动 |
| `internal/service/indexer.go` | 修改 `UpdateCollection`：拆分 glob pattern、按扩展名选 chunker |
| `internal/cli/collection.go` | 默认 mask 改为 `**/*.{md,txt}` |
| `internal/daemon/routes.go` | 默认 mask 改为 `**/*.{md,txt}` |

## PlainTextChunker 算法

```
输入: body string, chunkSize=300, overlap=15%

1. 若 rune 数 ≤ chunkSize，返回单个 chunk
2. 从位置 0 开始，取 chunkSize 个 rune 作为候选切割点
3. 在候选点前后各 50 rune 范围内，找最近的句号（。.！！？？）或换行符（\n）
4. 在该位置切割，形成 chunk
5. 下一个 chunk 的起点 = 切割点 - overlap
6. 重复直到结束
7. 合并过小的尾部 chunk（< chunkSize/4）
```

与 MarkdownChunker 的关键区别：
- 无 breakPoints 扫描（不感知标题、代码块）
- 无 base64 图片剔除
- 无 codeFence 检测

## 不涉及

- PDF 转换（外部 Agent 职责）
- PDF 文件监控/自动转换
- 新的 DB schema
- HTML、代码文件、配置文件等其它文件类型
