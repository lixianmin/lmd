**[English Documentation](README.md)**

# LMD - 本地 Markdown 文档搜索引擎

一个 Go 语言编写的本地混合搜索引擎，专注于 Markdown 文档管理，提供一流的中文语言支持。

LMD 结合 **BM25 关键词搜索**（基于 FTS5 + gse 分词）和**向量语义搜索**（基于 sqlite-vec），为你的 Markdown 知识库提供快速、精准的检索能力。同时提供 CLI 工具和可引用的 Go 代码库。

## 特性

- **混合搜索**：BM25 关键词搜索 + 向量语义搜索，后续计划支持 RRF 融合排序
- **中文优先**：gse 分词器提供准确的中文分词能力
- **Markdown 感知**：分块时尊重标题和代码块边界
- **单二进制文件**：编译即可运行，无需外部服务
- **Go 代码库**：通过 `pkg/` 包可编程调用
- **Agent 就绪**：计划支持 MCP Server + JSON 输出，便于 AI Agent 集成

## 安装

```bash
go install -tags "fts5" github.com/lixianmin/lmd/cmd/lmd@latest
```

或从源码构建：

```bash
git clone https://github.com/lixianmin/lmd.git
cd lmd
make install
```

> **注意：** 需要 `fts5` 编译标签以支持全文搜索。同时需要 CGo 和 C 编译器（GCC/Clang）。

## 快速开始

```bash
# 添加文档集合
lmd collection add ~/notes --name mynotes

# 索引所有集合（扫描新增/修改/删除的文件）
lmd update

# BM25 关键词搜索
lmd search "并发编程"
lmd search "goroutine channel" -n 10

# 向量语义搜索
lmd embed
lmd vsearch "并发编程模式"

# 查看文档
lmd get mynotes/go.md
lmd get "#abc123"

# 查看状态
lmd status
```

## CLI 命令参考

| 命令 | 说明 |
|------|------|
| `collection add <path>` | 添加文档集合 |
| `collection list` | 列出所有集合 |
| `collection remove <name>` | 删除集合 |
| `collection rename <old> <new>` | 重命名集合 |
| `update` | 扫描文件系统并更新索引 |
| `embed` | 为文档块生成向量嵌入 |
| `search <query>` | BM25 关键词搜索 |
| `vsearch <query>` | 向量语义搜索 |
| `get <collection/path>` 或 `get <#docid>` | 获取文档 |
| `status` | 查看索引状态 |

### 参数

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `--index` | `~/.cache/lmd/index.sqlite` | 数据库文件路径 |
| `--collection, -c` | 全部 | 限定搜索范围 |
| `--limit, -n` | 5 | 返回结果数量 |
| `--full` | false | 显示完整文档内容 |
| `--min-score` | 0 | 最低分数阈值 |


## 项目结构

```
cmd/lmd/              CLI 入口
internal/cli/         Cobra 命令定义
internal/service/     业务逻辑（索引、搜索、嵌入）
internal/store/       SQLite 持久化（FTS5 + sqlite-vec）
internal/tokenizer/   文本分词（gse）
internal/embedding/   向量嵌入抽象层
internal/chunker/     Markdown 感知的文档分块
test/fixtures/        测试文档（中文 + 英文）
```

## 技术栈

| 组件 | 库 | 用途 |
|------|-----|------|
| CLI | [cobra](https://github.com/spf13/cobra) | 命令框架 |
| SQLite | [go-sqlite3](https://github.com/mattn/go-sqlite3) | 数据库（WAL 模式） |
| 全文搜索 | FTS5 + [gse](https://github.com/go-ego/gse) | BM25 + 中文分词 |
| 向量搜索 | [sqlite-vec](https://github.com/asg017/sqlite-vec) | KNN 向量相似度 |
| 嵌入模型 | Mock（计划使用 Qwen3） | 向量嵌入生成 |

## 开发

```bash
make build          # 编译
make test           # 运行测试
make test-verbose   # 运行测试（详细输出）
make vet            # 静态分析
make lint           # vet + fmt
make e2e            # 端到端测试
make all            # lint + test + build
make clean          # 清理编译产物
```

## 许可证

MIT
